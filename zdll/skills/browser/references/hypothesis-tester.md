---
description: 浏览器漏洞假设验证：构造最小 PoC、使用 d8/v8 shell、AddressSanitizer 崩溃分析、调试器验证
tags: [browser, hypothesis-testing, d8, v8, ASAN, GDB, PoC, fuzzing]
---

# 浏览器假设验证者 (Browser Hypothesis Tester)

## Triggers — 专家直觉触发器

在验证假设阶段，以下现象应立即改变你的优先级：

1. **Debug 构建触发 `DCHECK` 失败，但 Release 构建未崩溃**：这是高概率的漏洞前兆，说明运行时安全机制在 Release 中被剥离。
2. **AddressSanitizer 报告 `heap-use-after-free` 且释放栈与使用栈跨越多个 JS 事件循环**：GC 相关的 UAF，利用价值极高。
3. **d8 shell 中 `%DebugPrint(obj)` 显示的 Map 类型与 TurboFan 优化假设不符**：确认类型推断错误的铁证。
4. **同一 PoC 在 `--no-opt` 下正常，在默认优化下崩溃**：明确指向 JIT 优化管道的漏洞。
5. **Mojo 接口 fuzz 后浏览器进程日志出现 `Received bad message` 但未终止渲染进程**：说明存在非法消息被部分处理，状态机可能已污染。
6. **WASM 模块在 `wasm-objdump` 中显示合法，但 V8 验证器（Validator）抛出内部错误**：验证器与执行器之间的分歧可能导致非法指令被执行。

## Question Chain — 不可跳过的问题链

Q1: **该假设是否能在最小的 JavaScript 片段中独立复现？**
- 如果不能，说明外部依赖过多，需要剥离 HTML/CSS/其他脚本，直到只剩下核心触发逻辑。

Q2: **崩溃是否在关闭 TurboFan（`--no-turbofan`）或 Ignition（`--no-ignition`）后消失？**
- 这能精确锁定漏洞所在的编译管道层级。

Q3: **ASAN / MSAN 报告的内存访问地址与 PoC 中构造的对象布局是否一致？**
- 例如：ASAN 报告访问地址 `0x41414141`，而 PoC 中恰好构造了一个值为 `0x41414141` 的数组元素——说明已实现有限控制。

Q4: **该崩溃是否 100% 可复现？若存在竞争，时间窗口是多少毫秒级别？**
- 使用 `--repeat` 和自动化脚本统计成功率。低于 10% 的崩溃可能需要堆风水或线程调度辅助。

Q5: **d8 中的行为与完整 Chromium 中的行为是否一致？**
- 有时 Blink 的绑定层会引入额外的安全检查，导致 d8 中可触发的问题在 Chromium 中被拦截。

Q6: **Mojo 消息的非法序列是否导致浏览器进程进入非预期状态（如句柄泄漏、文件未关闭）？**
- 崩溃不是唯一指标，状态污染同样危险。

## Attack Chain Closure — 攻击链闭合逻辑

### 假设验证的因果闭环

```
提出假设（某优化阶段错误消除了检查）
    ↓
构造最小 PoC（剥离所有无关代码，保留单一变量）
    ↓
控制实验（开关编译选项、对比 Debug/Release、对比 d8/Chromium）
    ↓
收集客观证据（ASAN 日志、%DebugPrint 输出、GDB 断点触发）
    ↓
判定假设真伪（Validated / Refuted / Inconclusive）
    ↓
若 Validated：评估利用潜力 → 进入 Validator 阶段
若 Refuted：记录反例，修正假设 → 重新提出假设
```

### 验证层级金字塔

1. **逻辑层验证**：通过阅读源码确认假设的代码路径存在。
2. **单元层验证**：在 d8 中构造 JS 片段，验证优化行为是否符合预期。
3. **集成层验证**：在完整 Chromium 中运行 HTML PoC，确认 Blink 绑定层未拦截。
4. **崩溃层验证**：使用 ASAN/MSAN 构建，确认内存破坏发生。
5. **控制层验证**：通过修改对象内容（如 `0x41414141`），确认崩溃地址可控。

## Code Examples — 脆弱模式与安全模式

### 1. d8 最小验证脚本

```js
// 假设：TurboFan 在特定类型变化后错误消除 CheckMaps
// 保存为 test.js，运行：d8 --allow-natives-syntax --turbo test.js

function trigger(obj) {
  // 假设优化器认为 obj.x 总是 Smi
  return obj.x + 1;
}

// 预热触发优化
for (let i = 0; i < 100000; i++) {
  trigger({x: i});
}

// 改变 Map，打破假设
let evil = {x: 1.1};  // HeapNumber，而非 Smi
%OptimizeFunctionOnNextCall(trigger);
trigger(evil);

// 验证：使用 %DebugPrint 查看对象 Map
%DebugPrint(evil);
// 若崩溃或输出异常，说明假设成立
```

```bash
# 安全对照实验：关闭优化后不应崩溃
d8 --allow-natives-syntax --no-turbo test.js
```

### 2. GC UAF 最小验证（HTML）

```html
<!-- 假设：在 FinalizationRegistry 回调中访问已释放对象 -->
<script>
let target = {};
let ref = new FinalizationRegistry((heldValue) => {
  // 假设：heldValue 或全局闭包中仍引用已释放的 C++ 对象
  console.log(heldValue);
});

ref.register(target, "secret");

// 强制 GC
for (let i = 0; i < 1000; i++) {
  new Array(10000).fill(0);
}
target = null;

// 再次强制 GC，触发回调
for (let i = 0; i < 1000; i++) {
  new Array(10000).fill(0);
}
</script>
```

```bash
# 使用 ASAN 构建的 Chromium 运行
# 若出现 heap-use-after-free，假设成立
./chrome --enable-features=MemorySaver --no-sandbox poc.html
```

### 3. GDB 调试 JIT 生成代码

```bash
# 附加到 Chromium 渲染进程
gdb -p $(pgrep -n chrome)

# 在 TurboFan 优化函数入口设置断点
(gdb) break v8::internal::Runtime_CompileOptimized_OSRTryInstall

# 运行 PoC，命中断点后，获取优化代码地址
(gdb) x/10i $pc

# 单步跟踪 JIT 代码执行，观察是否跳过 CheckBounds
(gdb) si
(gdb) info registers rax
```

```bash
# 安全对照：在 CheckBounds 节点处设置硬件断点
(gdb) break *0x<optimized_code_address> + <offset>
# 若优化后该断点永不被触发，说明 CheckBounds 确实被消除
```

### 4. Mojo 消息序列验证（Python）

```python
# 假设：乱序 Mojo 消息可导致浏览器进程状态污染
# 使用 Chromium 的 mojo Python 绑定构造消息

from mojo import core
from generated.mojom import file_system_manager

# 创建消息管道
pipe = core.MessagePipe()
proxy = file_system_manager.FileSystemManagerProxy(pipe.handle0)

# 发送消息 B（假设需要前置状态 A）
proxy.OpenFile("/etc/passwd", lambda f: print(f))
# 故意不发送消息 A

# 观察浏览器进程日志：是否出现未初始化状态访问
```

```python
# 安全对照：发送正确顺序的消息，确认正常行为
proxy.InitializeOrigin("https://trusted.com")
proxy.OpenFile("/trusted/path", lambda f: print(f))
```

### 5. WASM 验证模块

```wat
;; 假设：V8 WASM 验证器允许非法内存访问
(module
  (memory 1)
  (func (export "test") (param i32)
    ;; 尝试使用参数作为偏移量加载内存
    local.get 0
    i32.load
    drop
  )
)
```

```js
// JS 调用验证
const wasmModule = new WebAssembly.Module(wasmBuffer);
const instance = new WebAssembly.Instance(wasmModule);
// 传入超大偏移量
instance.exports.test(0xFFFFFFFF);  // 若未触发边界检查，假设成立
```

```js
// 安全对照：期望抛出 RangeError
try {
  instance.exports.test(0xFFFFFFFF);
} catch (e) {
  console.assert(e instanceof RangeError);
}
```

---

**核心原则**：假设验证不是“试到崩溃为止”，而是严格的控制实验。每一次实验都必须有对照组，每一次崩溃都必须回答：我控制了什么变量？这个变量与崩溃的因果关系是什么？如果不能回答，就只是在 fuzzing，而不是在验证。
