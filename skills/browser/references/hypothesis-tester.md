---
description: 假设验证 - 快速验证假设过滤假阳性
---

# 浏览器假设验证 (Hypothesis Tester)

快速验证关于 JS 引擎优化或 DOM 生命周期的猜想。

## 验证目标

1.  **JIT 优化猜想**: "如果我强制让这个变量变成 Double 类型，TurboFan 的 Range Analysis 还会生效吗？"
2.  **生命周期猜想**: "在 `NodeRemoved` 事件中调用 `gc()`，这个对象真的被回收了吗？"
3.  **沙箱猜想**: "这个 Mojo 接口是否允许跨域调用？"

## 验证方法

### 1. DCHECK 验证 (Debug Build)
使用 Debug 版本的浏览器运行 PoC：
*   **触发 Assert**: 观察是否触发了 `DCHECK` 失败。Assert 往往是 Release 版本中漏洞的前兆。

### 2. JS 状态探测
*   **`%DebugPrint()`**: 打印对象的 Map 和内存地址（需 `--allow-natives-syntax`）。
*   **ArrayBuffer 探测**: 检查 TypedArray 的 length 是否被恶意修改。

## 输出格式
```
## 验证报告

### 假设
[描述假设]

### 验证过程
- 工具: [Frida/GDB]
- 脚本: [Script snippet]
- 观察: [Log output]

### 结论
[Validated / Refuted]
```