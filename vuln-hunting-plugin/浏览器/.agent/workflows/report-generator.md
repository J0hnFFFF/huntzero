---
description: 报告生成 - 合成专业报告
---

# 浏览器分析报告 (Report Generator)

## 报告结构

### 1. 概览
*   浏览器版本 (V8 版本 / Build 号)
*   启动参数 (Flags)
*   Crash 类型 (UAF/OOB/Assert)

### 2. 漏洞详情
*   **ASAN Report**: AddressSanitizer 的完整输出日志。
*   **JS PoC**: 最小化触发代码。
*   **Root Cause Analysis**: 深入引擎源码的分析，指出具体的 C++ 代码错误。

### 3. 可利用性评估
*   **DCHECK vs Release**: 是否仅在 Debug 版本触发？
*   **Sandbox**: 是否需要配合沙箱逃逸？

### 4. 修复建议
*   补丁建议 (e.g. 增加 CheckBound 节点)。