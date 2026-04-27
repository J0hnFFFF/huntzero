---
description: 邮件服务器领域情报简报 (V7 Domain Intelligence Brief)
---

# 邮件系统地形情报 (Mail Domain Brief)

> [!IMPORTANT]
> V7 模式：本文件是 **领域地形知识**，不是执行步骤。
> Cerebrum 读取后应基于第一性原理自主决定分析路径。

## 邮件系统的本质结构

邮件服务器是一类**以协议状态机为核心**的服务系统，特征是：

- **主要 Invariant**：只有经过合法认证的主体才能投递/读取属于其账户的邮件内容。协议状态机的任何转换只能在当前状态允许的命令集范围内发生。
- **核心状态**：SMTP/IMAP 会话状态（`EHLO`, `AUTH`, `DATA`, `IDLE` 等）、邮件队列状态（`queued/delivered/bounced`）
- **核心转换**：客户端发送的每条协议命令驱动会话状态从一个节点转移到下一个节点；协议标准（RFC）定义了合法转换图

## 已知的结构性地形

### 1. 协议解析层（协议命令解析器）
邮件协议解析器是最高价值的分析区域：
- **命令分发器**：将原始字节流映射到协议命令的分发函数——缓冲区大小、命令参数解析
- **MIME 解析器**：多部分邮件体的解析涉及递归结构，边界字符串处理是历史高发区
- **TNEF/iCal 解析**：特殊附件格式的解析器通常测试覆盖率低，而输入完全由外部控制

### 2. 认证与授权的 Invariant 声明
- **SASL 机制**：认证机制的选择与执行——PLAIN/LOGIN vs GSSAPI/NTLM 不同实现的安全基线差异巨大
- **Pre-auth 攻击面**：在认证完成之前可接受的命令集范围——EHLO、STARTTLS 握手期间的代码路径特别重要
- **中继策略（Relay Policy）**：开放中继是策略级别的 Invariant 失效，检查 `mynetworks` / `smtpd_recipient_restrictions`

### 3. 进程架构与权限分离
- **多进程模型**（Postfix 典型架构）：smtpd → cleanup → qmgr → smtp 各进程权限不同
- **LDA（本地投递代理）**：最终写入 Maildir/Mbox 的组件，运行在目标用户权限下，是提权分析的关键点
- **Queue 文件格式**：队列文件的路径构造逻辑——历史上存在路径遍历导致任意文件写入的案例

## 已知的 Invariant 失效模式

| Invariant 声明 | 常见失效原因 | 表现位置 |
|----------------|-------------|---------|
| "协议命令必须按序执行" | 接受乱序命令且状态不回滚 | SMTP 会话状态机 |
| "只有认证用户才能投递" | 中继策略配置缺陷或绕过 | smtpd_recipient_restrictions |
| "MIME 边界明确划分内容" | 边界字符串解析的差异导致内容注入 | MIME 多部分解析器 |
| "附件内容不影响服务器逻辑" | 特殊格式附件触发服务器端解析器漏洞 | TNEF/iCal/vCard 解析 |

## 标准分析产物路径
- 发现写入：`.audit_notes.md`
- 协议流记录：`local_workspace/protocol_traces/`
- 验证脚本：`local_workspace/` 下的 Python PoC
