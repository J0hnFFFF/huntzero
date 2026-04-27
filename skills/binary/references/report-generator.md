---
description: 报告生成 - 合成专业报告
---

# 二进制分析报告 (Report Generator)

## 报告结构

### 1. 概览
*   文件 SHA256
*   安全保护机制 (Mitigations) 状态
*   发现的漏洞类型与数量

### 2. 漏洞详情
*   **Crash State**: 崩溃时的寄存器与栈快照。
*   **Root Cause**: 伪代码层面的逻辑分析。
*   **Exploitation**: 利用路径简述 (e.g. Stack Overflow -> ROP -> Shell)。

### 3. 可利用性评估
*   是否受 ASLR 影响？
*   利用稳定性评级。

### 4. 修复建议
*   代码修复 (e.g. 使用 `strncpy` 替代 `strcpy`)。
*   编译选项建议 (e.g. 开启 `PIE` 和 `SSP`)。