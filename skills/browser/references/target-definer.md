---
description: 浏览器引擎攻击面定义：JIT 管道入口、DOM API、IPC 接口、扩展 API、WebGL/WASM 边界
tags: [browser, attack-surface, JIT, DOM, IPC, WebGL, WASM, extensions]
---

# 浏览器目标定义者 (Browser Target Definer)

## Triggers — 专家直觉触发器

当遇到以下信号时，经验丰富的浏览器安全研究者会立即提高警惕：

1. **Chromium Commit 中出现 `New Web Platform Feature`**：新特性往往伴随未充分审计的绑定层和 IPC 接口。
2. **`*.mojom` 文件新增处理 `file_path`、`handle`、`shared_memory`**：这些类型跨越信任边界，序列化/反序列化错误可能导致沙箱逃逸。
3. **V8 提交涉及 `Typer::Operation` 或 `RangeType` 修改**：任何对类型推断或范围分析的修改都可能引入 JIT 类型混淆。
4. **Blink 中新增 `IDL` 接口且包含 `Promise`、`EventTarget`、`Callback`**：异步回调与 GC 交互极易产生 UAF。
5. **WebGL/WebGPU 增加新的纹理格式或缓冲区映射 API**：GPU 进程与渲染进程共享内存，映射生命周期管理错误可导致 OOB。
6. **扩展 API 新增 `chrome.*` 权限或 `nativeMessaging`**：扩展进程拥有更高权限，接口验证绕过可直接危害浏览器进程。

## Question Chain — 不可跳过的问题链

Q1: **该攻击面是否跨越渲染进程与浏览器进程（或 GPU 进程）的信任边界？**
- 若仅停留在渲染进程内部（如纯 JS JIT），影响上限为 Renderer RCE；若涉及 IPC，则存在 Sandbox Escape 潜力。

Q2: **该入口是否处理外部提供的复杂数据结构（如嵌套字典、文件句柄、共享内存描述符）？**
- 复杂结构意味着更多的解析代码和更深的调用栈，验证遗漏的概率指数级上升。

Q3: **该代码路径是否涉及 JIT 编译器的假设（类型、范围、边界检查）？**
- JIT 管道中的 Typer 和 Simplified Lowering 阶段若基于错误假设消除检查，运行时违例将直接转化为内存破坏。

Q4: **该 DOM API 是否允许在回调（事件、Promise、动画帧）中修改正在遍历的节点树？**
- 重入（Reentrancy）是 DOM UAF 的首要根源，特别是当回调触发 GC 时。

Q5: **WASM / WebGL 内存映射是否允许渲染进程控制映射偏移量或长度？**
- 用户可控的偏移量 + 整数溢出 = 经典的 GPU 进程内存越界读写。

Q6: **该扩展 API 是否将未验证的渲染进程起源（Origin）传递给浏览器进程执行特权操作？**
- 起源验证缺失是 UXSS 和权限提升的常见模式。

## Attack Chain Closure — 攻击链闭合逻辑

一个完整的浏览器攻击链必须回答：**从用户可控的输入字节，到任意代码执行，再到沙箱逃逸，逻辑是否自洽？**

### 逻辑证明模板

1. **入口确认**：攻击者控制的输入（HTML/JS/WASM/Mojo Message）能够到达目标代码路径。
   - 例：通过 `navigator.gpu.requestAdapter()` 触发 WebGPU 新路径。

2. **状态机破坏**：输入导致目标组件的内部状态机偏离设计假设。
   - 例：向 Mojo 接口发送乱序消息，使浏览器进程的 `FileSystemManager` 进入不一致状态。

3. **内存破坏**：状态机不一致导致类型混淆、UAF 或 OOB。
   - 例：JIT 优化基于 `HeapNumber` 假设，运行时却传入 `BigInt`，导致后续 `CheckBounds` 被错误消除。

4. **原语构建**：利用内存破坏构建稳定的读写原语（AddrOf / FakeObj / Arbitrary R/W）。
   - 例：通过篡改 `ArrayBuffer` 的 `backing_store` 指针实现任意地址读写。

5. **权限提升**：从渲染进程权限逃逸到浏览器进程或操作系统内核。
   - 例：劫持 Mojo 接口句柄，调用原本仅限浏览器进程调用的特权方法；或通过 Windows RPC 调用未过滤的 `Win32k` 接口。

## Code Examples — 脆弱模式与安全模式

### 1. Mojo IPC 接口定义（脆弱模式）

```mojom
// 脆弱：直接传递文件句柄且未验证来源
interface FileSystemManager {
  // 浏览器进程接收渲染进程传来的路径，若未验证 Origin，可导致任意文件读取
  OpenFile(string path) => (handle file);
};
```

```mojom
// 安全：增加 Origin 验证和路径规范化
interface FileSystemManager {
  OpenFile(url.mojom.Origin origin, string sanitized_path)
      => (handle file);
};
```

### 2. JIT 管道入口（脆弱模式）

```cpp
// v8/src/compiler/typer.cc
// 脆弱：假设输入总是 Smi（小整数），未处理 HeapNumber 情况
Type Typer::Visitor::TypeNumberAdd(Type lhs, Type rhs) {
  if (lhs.Is(Type::SignedSmall()) && rhs.Is(Type::SignedSmall())) {
    return Type::SignedSmall();  // 假设过于激进
  }
  return Type::Number();
}
```

```cpp
// 安全：保守推断，并在 Lowering 阶段保留 CheckBounds/CheckMaps
Type Typer::Visitor::TypeNumberAdd(Type lhs, Type rhs) {
  if (lhs.Is(Type::SignedSmall()) && rhs.Is(Type::SignedSmall())) {
    return Type::SignedSmall();
  }
  // 必须回退到通用路径，不能消除后续检查
  return Type::Number();
}
```

### 3. DOM API 绑定层（脆弱模式）

```cpp
// blink/renderer/core/dom/element.cc
// 脆弱：在 GC 可能触发的回调中裸用原始指针
void Element::RemoveChild(Node* child) {
  // ...
  if (child->parentNode() == this) {
    ContainerNode::RemoveChild(child);
    child->DispatchEvent(Event::Create(EventTypeNames::kDOMNodeRemoved));
    // child 可能在事件处理中被回收，后续访问 child 导致 UAF
  }
}
```

```cpp
// 安全：使用 GC 安全的 Persistent 或 Member 包装，并在事件分发后重新验证
void Element::RemoveChild(Node* child) {
  // ...
  Member<Node> protector = child;
  ContainerNode::RemoveChild(child);
  protector->DispatchEvent(Event::Create(EventTypeNames::kDOMNodeRemoved));
  // protector 保持 child 存活直到作用域结束
}
```

### 4. WebGL/WASM 边界（脆弱模式）

```cpp
// gpu/command_buffer/service/gles2_cmd_decoder.cc
// 脆弱：用户可控的 offset 与 size 相加时未检查溢出
error::Error GLES2DecoderImpl::DoTexSubImage2D(
    GLint offset, GLsizei size, const void* data) {
  GLsizei end = offset + size;  // 整数溢出！
  if (end > buffer_size_) {
    return error::kOutOfBounds;
  }
  memcpy(dst, data, size);
}
```

```cpp
// 安全：使用饱和加法或显式溢出检查
error::Error GLES2DecoderImpl::DoTexSubImage2D(
    GLint offset, GLsizei size, const void* data) {
  if (size < 0 || offset < 0) return error::kInvalidArguments;
  if (static_cast<size_t>(offset) + static_cast<size_t>(size) > buffer_size_) {
    return error::kOutOfBounds;
  }
  memcpy(dst, data, size);
}
```

### 5. 扩展 API（脆弱模式）

```cpp
// chrome/browser/extensions/api/foo/foo_api.cc
// 脆弱：未验证扩展 ID 即执行特权操作
ExtensionFunction::ResponseAction FooAPI::Run() {
  auto params = api::foo::DoPrivileged::Params::Create(args());
  DoPrivilegedOperation(params->path);  // 任何扩展都可调用？
  return RespondNow(NoArguments());
}
```

```cpp
// 安全：严格校验调用者权限与 Origin
ExtensionFunction::ResponseAction FooAPI::Run() {
  EXTENSION_FUNCTION_VALIDATE(extension_->permissions()->ContainsAPI(
      mojom::APIPermissionID::kPrivilegedFoo));
  auto params = api::foo::DoPrivileged::Params::Create(args());
  if (!ValidatePathWithinExtension(params->path, extension_)) {
    return RespondNow(Error("Invalid path"));
  }
  DoPrivilegedOperation(params->path);
  return RespondNow(NoArguments());
}
```

---

**核心原则**：攻击面定义不是简单的“功能列表”，而是对“用户输入 → 信任边界 → 状态机 → 内存操作”这一完整数据流的因果刻画。每一个新增接口都必须被追问：它是否允许不可信输入影响可信进程的状态机？

### 攻击面优先级速查表

| 优先级 | 攻击面特征 | 示例 |
|--------|-----------|------|
| Critical | 跨进程 + 复杂数据结构 + 用户可控 | WebGPU 内存映射、Mojo 文件接口 |
| High | 渲染进程内 + JIT/DOM 复杂交互 | TurboFan 新优化、SVG 解析器 |
| Medium | 扩展 API + 特权操作 | `chrome.downloads`、Native Messaging |
| Low | 信息泄露或拒绝服务 | 计时器精度、CSS 解析崩溃 |

### 版本差异影响

不同 Chromium 版本的攻击面存在显著差异。V8 的 Pointer Compression（指针压缩）从 V8 v7.8 开始引入，改变了对象内存布局；Site Isolation 从 Chrome 67 成为默认配置，将 Renderer RCE 的影响限制在单个站点内。审计时必须明确目标版本，避免将现代缓解措施下的不可利用条件误判为漏洞。
