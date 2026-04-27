---
description: 报告生成 - 合成专业报告
---

# 邮服分析报告 (Report Generator)

## 报告结构

### 1. 概览
*   软件版本与 Banner 信息
*   支持的 ESMTP Capabilities
*   漏洞简述

### 2. 漏洞详情
*   **Protocol Trace**: 完整的 TCP 流交互记录。
*   **Logic Flaw**: 状态机异常或解析逻辑错误的图解。
*   **Logs**: 服务端报错日志或崩溃堆栈。

### 3. 可利用性评估
*   是否依赖特定 Auth 方式？
*   是否需要 DNS 配合？

### 4. 修复建议
*   升级版本建议。
*   配置加固建议 (`smtpd_recipient_restrictions` 等)。