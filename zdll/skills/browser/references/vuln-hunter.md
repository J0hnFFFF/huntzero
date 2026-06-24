---
description: 浏览器深层漏洞挖掘：JIT 类型混淆、GC UAF、Mojo 状态机、DOM Clobbering、原型污染、WASM 内存安全
tags: [browser, vulnerability-hunting, JIT, GC, Mojo, DOM, prototype-pollution, WASM]
---

# 浏览器漏洞挖掘者 (Browser Vuln Hunter)

## Triggers — 专家直觉触发器

以下代码或行为模式出现时，应立即触发深度审计直觉：

1. **V8 `Typer::Operation` 中返回 `Type::None()` 或 `Range` 收缩过于激进**：这意味着优化器假设了过于狭窄的运行时状态。
2. **Blink 中存在 `ClearChildren()` 后立即 `DispatchEvent()` 的模式**：事件处理可能重新触发脚本执行，导致已标记为死亡的对象被重新访问。
3. **Mojo 实现类中存在未处理 `OnConnectionError()` 的回调**：连接断开后的状态清理缺失，可导致后续消息处理在已释放的上下文上执行。
4. **DOM API 通过 `AttributeChanged` 回调直接修改 `innerHTML` 或调用 `appendChild`**：属性变更回调重入 DOM 树修改，极易引发布局期间的 UAF。
5. **JS 全局对象或内置原型（`Object.prototype`、`Array.prototype`）在浏览器上下文下可被脚本修改**：DOM Clobbering 和原型污染的温床，特别是在扩展内容脚本隔离不严格时。
6. **WASM 模块导入函数指针未经 `sig` 签名检查直接调用**：类型混淆可导致将整数解释为函数指针，跳转到任意地址。

## Question Chain — 不可跳过的问题链

Q1: **JIT 编译器在哪个优化阶段基于什么假设消除了安全检查？**
- 检查 `Typer`、`SimplifiedLowering`、`EscapeAnalysis` 的变更日志，确认假设是否有反例。

Q2: **是否存在一条 JS 执行路径，能够在 GC 周期内使 C++ 侧的 `Member<T>` 与 V8 侧的 `Persistent` 引用计数失步？**
- 特别关注跨上下文（Cross-Context）对象传递和 `PostTask` 异步回调。

Q3: **Mojo 消息处理函数是否假设了消息顺序？是否存在不合法的乱序消息被静默接受而非主动断开连接？**
- 状态机漏洞的核心是：非法输入被接受并修改了状态，而非触发错误处理。

Q4: **该 DOM API 是否允许通过 HTML 属性名（如 `id`、`name`）覆盖全局变量，从而干扰安全敏感的内置逻辑？**
- DOM Clobbering 的攻击不需要 JS 执行权限，纯 HTML 即可构造。

Q5: **WASM 线性内存（Linear Memory）与宿主（Host）之间的边界检查是否在 32 位和 64 位模式下保持一致？**
- 64 位模式下指针宽度变化可能导致旧的边界检查逻辑失效。

Q6: **补丁修复是否仅针对特定调用点，而忽略了同源的其他代码路径？**
- 补丁间隙（Patch Gap）分析：修复一个文件中的 `RefPtr` 使用，但同目录下 10 个类似文件未修改。

## Attack Chain Closure — 攻击链闭合逻辑

### JIT 类型混淆链

```
JS 类型变化（Smi → HeapNumber → BigInt）
    ↓
TurboFan 基于旧类型信息优化（CheckMaps 被消除）
    ↓
运行时类型不匹配，但无检查点 → 访问错误偏移的内存
    ↓
类型混淆（Type Confusion）：对象 A 的内存被解释为对象 B
    ↓
构造 AddrOf / FakeObj 原语
```

### GC UAF 链

```
C++ 对象进入析构或清理阶段
    ↓
清理逻辑触发 JS 回调（Finalizer / WeakCallback / Event）
    ↓
JS 回调通过闭包或全局变量再次引用正在清理的对象
    ↓
Oilpan GC 已完成标记，认为对象可回收
    ↓
JS 回调后续访问已释放内存 → UAF
```

### Mojo 状态机链

```
攻击者控制渲染进程发送 Mojo 消息序列
    ↓
消息 A 使浏览器进程进入“半初始化”状态（如打开文件句柄但未验证）
    ↓
消息 B 在非法状态下被处理（如句柄已打开但权限未校验）
    ↓
浏览器进程执行本不该允许的操作（如读取任意文件）
    ↓
沙箱逃逸完成
```

## Code Examples — 脆弱模式与安全模式

### 1. JIT 类型混淆（脆弱模式）

```cpp
// v8/src/compiler/simplified-lowering.cc
// 脆弱：基于 Typer 的 Range 分析消除 CheckBounds
void SimplifiedLowering::LowerCheckBounds(Node* node) {
  if (upper_bound < length_min) {
    // 认为索引一定在范围内，直接替换为纯索引节点
    ReplaceWithValue(node, index);
    return;
  }
  // ...
}
```

```cpp
// 安全：即使范围分析显示安全，也保留显式检查或去优化点
void SimplifiedLowering::LowerCheckBounds(Node* node) {
  if (upper_bound < length_min) {
    // 保守策略：保留 CheckBounds 节点，让后端决定是否优化
    return;
  }
  // ...
}
```

### 2. GC UAF（脆弱模式）

```cpp
// blink/renderer/core/svg/svg_element.cc
// 脆弱：在析构或清理中分发事件，未保护 this
SVGElement::~SVGElement() {
  if (HasEventListeners()) {
    DispatchEvent(Event::Create(EventTypeNames::kUnload));
    // this 可能在此回调中被脚本重新引用，但析构已进行
  }
}
```

```cpp
// 安全：避免在析构函数中执行脚本；若必须，使用 PreFinalizer
class SVGElement : public Element {
  void Dispose();  // 在 GC 标记前由 PreFinalizer 调用
};

void SVGElement::Dispose() {
  // 此时对象仍存活，可安全分发事件
  if (HasEventListeners()) {
    DispatchEvent(Event::Create(EventTypeNames::kUnload));
  }
}
```

### 3. Mojo 状态机（脆弱模式）

```cpp
// chrome/browser/file_system/file_system_manager_impl.cc
// 脆弱：允许在验证前执行操作
void FileSystemManagerImpl::OpenFile(const GURL& path,
                                      OpenFileCallback callback) {
  // 先打开文件，再验证 Origin
  base::File file(path);
  if (!ValidateOrigin(origin_)) {
    std::move(callback).Run(base::File());
    return;
  }
  std::move(callback).Run(std::move(file));
}
```

```cpp
// 安全：严格顺序——先验证，再执行
void FileSystemManagerImpl::OpenFile(const GURL& path,
                                      OpenFileCallback callback) {
  if (!ValidateOrigin(origin_)) {
    mojo::ReportBadMessage("Invalid origin");
    std::move(callback).Run(base::File());
    return;
  }
  base::File file(path);
  std::move(callback).Run(std::move(file));
}
```

### 4. DOM Clobbering（脆弱模式）

```html
<!-- 脆弱：依赖全局变量 form 指向特定的 HTMLFormElement -->
<script>
  // 攻击者通过 HTML 注入：
  // <form id="form" action="javascript:alert(1)"></form>
  // form.action 被 HTML 元素覆盖，不再指向字符串
  if (form.action.startsWith('/trusted')) {
    fetch(form.action);  // 实际执行 javascript:alert(1)
  }
</script>
```

```html
<!-- 安全：使用局部变量并验证类型 -->
<script>
  const form = document.getElementById('trustedForm');
  if (!(form instanceof HTMLFormElement)) return;
  const action = String(form.action);
  if (action.startsWith('/trusted')) {
    fetch(action);
  }
</script>
```

### 5. WASM 内存安全（脆弱模式）

```cpp
// v8/src/wasm/wasm-objects.cc
// 脆弱：64 位下 size_t 与 uint32_t 混用导致截断
void WasmMemoryObject::Grow(uint32_t delta_pages) {
  uint32_t old_pages = memory_->pages();
  uint32_t new_pages = old_pages + delta_pages;  // 32 位溢出！
  memory_->Resize(new_pages * kWasmPageSize);    // 实际分配小于期望
}
```

```cpp
// 安全：使用饱和算术并限制最大值
void WasmMemoryObject::Grow(uint32_t delta_pages) {
  uint32_t old_pages = memory_->pages();
  uint32_t max_pages = memory_->maximum_pages();
  if (delta_pages > max_pages || old_pages > max_pages - delta_pages) {
    throw new RangeError("Memory grow failed");
  }
  uint32_t new_pages = old_pages + delta_pages;
  memory_->Resize(static_cast<size_t>(new_pages) * kWasmPageSize);
}
```

### 6. 原型污染（脆弱模式）

```js
// 扩展或内部页面中不安全地合并配置对象
function mergeConfig(defaults, userInput) {
  for (let key in userInput) {
    defaults[key] = userInput[key];  // 若 key 为 __proto__，则污染原型
  }
}
```

```js
// 安全：使用 Object.create(null) 或显式拒绝危险 key
function mergeConfig(defaults, userInput) {
  const safeDefaults = Object.create(null);
  Object.assign(safeDefaults, defaults);
  for (let key of Object.keys(userInput)) {
    if (key === '__proto__' || key === 'constructor') continue;
    safeDefaults[key] = userInput[key];
  }
}
```

---

**核心原则**：浏览器漏洞挖掘不是寻找“明显的空指针”，而是在“状态机假设”与“运行时现实”之间寻找语义鸿沟。JIT 假设了类型不变性，GC 假设了引用一致性，Mojo 假设了消息顺序性——打破这些假设，就是零日的诞生之地。
