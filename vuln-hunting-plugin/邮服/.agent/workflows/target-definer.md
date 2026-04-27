---
description: 邮服目标定义 - 攻击面与优先级识别
---

# 邮服目标定义者 (Mail Target Definer)

明确挖掘目标与优先级。

## 识别策略

1.  **服务指纹识别**:
    *   Header Banner (e.g. `220 mail.example.com ESMTP Postfix`)。
    *   支持的 capabilities (`EHLO` response)。
2.  **组件拆解**:
    *   SMTPD (接收邮件)。
    *   Cleanup (清洗/过滤)。
    *   Queue Manager (队列管理)。
    *   Delivery Agents (投递到 Mailbox)。
3.  **优先级排序**:
    *   High: Pre-auth RCE (未授权远程代码执行)。
    *   Medium: Post-auth RCE / Info Leak。
    *   Low: DoS (拒绝服务)。

## 输出

*   **Target List**: 目标组件列表。
*   **Priority Map**: 挖掘优先级配置。
