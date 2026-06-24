---
description: 浏览器漏洞五维评估：可利用性、稳定性、爆炸半径、Renderer 妥协、沙箱逃逸、系统妥协
tags: [browser, validation, exploitability, stability, sandbox, ASLR, DEP, CET]
---

# 浏览器评估验证者 (Browser Validator)

## Triggers — 专家直觉触发器

评估漏洞价值时，以下特征直接决定优先级：

1. **崩溃点位于 V8 堆（`v8::internal::HeapObject`）且能控制对象 Map 指针**：高概率转化为类型混淆，进而获得 AddrOf/FakeObj。
2. **ASAN 报告 `WRITE of size 8` 且目标地址与 PoC 中的 `ArrayBuffer` 地址相邻**：说明存在线性溢出，可覆盖相邻对象的 `length` 或 `backing_store`。
3. **漏洞触发无需用户交互（No User Interaction）且可在 iframe 中静默触发**：UXSS 和 Drive-by Download 的关键指标。
4. **Mojo 漏洞影响浏览器进程且可利用接口拥有 `FilePath` 或 `NetworkService` 权限**：即使需要 Renderer RCE 作为前置，沙箱逃逸路径也已打通。
5. **漏洞在 Site Isolation 开启后仍可导致跨站点数据读取**：说明隔离边界被突破，严重性高于普通 Renderer RCE。
6. **漏洞触发路径不涉及 JIT 编译（如纯 DOM 解析路径），但可获得任意读写**：这类漏洞往往更稳定，因为不依赖编译状态的不确定性。

## Question Chain — 不可跳过的问题链

Q1: **该崩溃能否转化为稳定的读原语？写原语？控制流劫持？**
- 读/写/控制流是三级跳，每一级都需要独立的验证。某些漏洞只能读，某些只能写，必须明确边界。

Q2: **触发成功率在 100 次独立运行中是多少？是否需要堆风水（Heap Feng Shui）？**
- 需要堆风水的漏洞在真实攻击场景中成功率下降，因为现代分配器（PartitionAlloc、TCMalloc）引入随机化。

Q3: **该漏洞在最新稳定版 Chromium / Chrome 上是否仍可复现？是否需要特定的 Flag 或企业策略关闭？**
- 若默认开启的防护（如 Site Isolation、CET）即可阻止利用，则实际威胁降级。

Q4: **从 Renderer 进程到浏览器进程的逃逸路径是否明确？需要几个 Mojo 接口漏洞串联？**
- 单接口漏洞价值远高于需要 3+ 个漏洞串联的链。

Q5: **该漏洞的爆炸半径（Blast Radius）是多少？影响所有平台（Windows/macOS/Linux/Android）还是仅限特定架构？**
- 跨平台漏洞价值高于单平台，但单平台内核漏洞（如 Android GPU 驱动）在特定场景下同样致命。

Q6: **该漏洞是否可被自动化检测（如 fuzzer）轻易发现？补丁修复是否涉及大面积重构？**
- 若修复仅需加一行边界检查，说明漏洞模式简单，变体可能存在；若修复重构了整个状态机，则回归风险低。

## Attack Chain Closure — 攻击链闭合逻辑

### 五维评估矩阵

| 维度 | 关键指标 | 高价值特征 |
|---|---|---|
| **Exploitability** | 读/写/控制流劫持 | Arbitrary R/W + PC Control |
| **Stability** | 成功率、环境依赖 | >90% 成功率，无需堆风水 |
| **Blast Radius** | 平台、版本、配置覆盖 | 全平台默认配置 |
| **Chainability** | 前置需求、后续衔接 | 单漏洞完成 Renderer → Browser |
| **Detectability** | 静态/动态检测难度 | 绕过现有 fuzzer 和规则 |

### 逻辑证明：从崩溃到系统妥协

```
崩溃点分析（Crash Triage）
    ↓
确认对象类型与内存布局（对象在 V8 堆中的偏移）
    ↓
构造稳定的 OOB / TypeConfusion → 篡改相邻对象（如 ArrayBuffer）
    ↓
ArrayBuffer backing_store 篡改 → Arbitrary R/W Primitive
    ↓
读取 WASM 模块的 RWX 内存页地址 或 V8 函数指针
    ↓
写入 Shellcode / ROP Chain → Renderer Process RCE
    ↓
分析 Renderer 拥有的 Mojo 接口 → 寻找未充分验证的 Broker 接口
    ↓
劫持 Mojo 句柄 → Sandbox Escape → Browser Process RCE
    ↓
浏览器进程权限分析 → 利用 OS 内核漏洞（如 Win32k / DWM）
    ↓
System Compromise
```

## Code Examples — 脆弱模式与安全模式

### 1. 可利用性评估：对象布局控制

```js
// 脆弱：通过类型混淆篡改 ArrayBuffer 长度
function corrupt() {
  let ab = new ArrayBuffer(0x100);
  let uint8 = new Uint8Array(ab);
  
  // 假设通过漏洞将 ab 的 length 字段改为 0xFFFFFFFF
  // 后续可实现任意地址读写
  let view = new DataView(ab);
  view.setUint32(0, 0x41414141, true);  // 实际写入 ab 内部地址 + 0
}
```

```js
// 安全：PartitionAlloc 的隔离区（Bucket）设计使相邻对象难以预测
// 现代缓解：V8 的 Pointer Compression 使 upper 32 位固定，降低任意写危害
// 评估时必须确认目标版本的 Pointer Compression 状态
```

### 2. 稳定性评估：统计脚本

```python
#!/usr/bin/env python3
import subprocess
import statistics

results = []
for i in range(100):
    proc = subprocess.run(
        ["./chrome", "--no-sandbox", "poc.html"],
        timeout=10,
        capture_output=True
    )
    if b"ERROR: AddressSanitizer" in proc.stderr:
        results.append("crash")
    elif proc.returncode == 0:
        results.append("survive")
    else:
        results.append("other")

success_rate = results.count("crash") / len(results)
print(f"Crash Rate: {success_rate:.2%}")
print(f"Stability: {'High' if success_rate > 0.9 else 'Medium' if success_rate > 0.5 else 'Low'}")
```

### 3. 沙箱逃逸评估：Mojo 接口权限审计

```cpp
// 评估代码：检查接口是否验证调用者权限
// chrome/browser/file_system/file_system_manager_impl.cc

// 脆弱：未验证即暴露特权操作
void FileSystemManagerImpl::ReadFile(const base::FilePath& path,
                                      ReadFileCallback callback) {
  // 若此处缺少 origin 检查，则 Renderer 可读取任意文件
  ReadFileImpl(path, std::move(callback));
}
```

```cpp
// 安全：多层级验证
void FileSystemManagerImpl::ReadFile(const base::FilePath& path,
                                      ReadFileCallback callback) {
  // 1. 验证消息来源的 Origin
  if (!HasPermissionForPath(path, origin_)) {
    mojo::ReportBadMessage("Unauthorized file access attempt");
    std::move(callback).Run(base::File::Error::FILE_ERROR_ACCESS_DENIED);
    return;
  }
  // 2. 验证路径在沙箱内
  if (!IsPathWithinSandbox(path)) {
    std::move(callback).Run(base::File::Error::FILE_ERROR_ACCESS_DENIED);
    return;
  }
  ReadFileImpl(path, std::move(callback));
}
```

### 4. 防护对抗评估：CET / Shadow Stack

```cpp
// 脆弱：传统的 ROP/JOP 链在 CET 启用时失效
// 因为 RET 指令必须匹配 ENDBR64 标签
void* rop_chain[] = {
  (void*)pop_rdi_gadget,
  (void*)0xdeadbeef,
  (void*)system_plt,
};
// CET 启用时：非法的 indirect jump 会导致 #CP 异常
```

```cpp
// 安全（攻击者视角）：使用 COP (Call-Oriented Programming) 或破坏 Shadow Stack
// 评估漏洞时必须确认目标是否启用 CET
// Windows 11 22H2+ / Linux 6.x+ 的 Chrome 通常启用 CET
// 若漏洞仅提供 PC 控制，但未提供 Shadow Stack 的写能力，则利用链断裂
```

### 5. Site Isolation 评估

```js
// 脆弱：跨站点数据读取绕过 Site Isolation
// 假设漏洞允许在一个 Renderer 中读取另一站点 iframe 的内存
// 由于不同站点通常在不同进程，这需要进程内隔离失效

// PoC：尝试读取 cross-origin iframe 中的敏感对象
let iframe = document.createElement('iframe');
iframe.src = 'https://bank.com/secret';
document.body.appendChild(iframe);

// 通过漏洞读取 iframe.contentWindow 的 JS 对象
// 若成功，说明 Site Isolation 未生效或存在绕过
```

```js
// 安全：Site Isolation 确保 cross-origin 页面在不同 Renderer 进程
// 评估时必须确认 chrome://process-internals 中是否分配了独立进程
// 若 PoC 仅在 same-process 模式下工作，则实际危害为 UXSS 而非信息泄露
```

---

**核心原则**：评估漏洞不是给它一个 CVSS 分数那么简单。真正的评估必须回答：在目标用户的实际环境中（默认配置、最新版本、启用所有缓解措施），这个漏洞能否被可靠地利用以达到攻击者的最终目标？如果答案依赖于多个不确定条件，那么它的实战价值必须被打折扣。
