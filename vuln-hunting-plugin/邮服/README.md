# Mail Server Module / 邮件服务器模块

[English](#english) | [中文](#中文)

---

<a id="english"></a>

## English

### About PreZero (猎零)

**PreZero (猎零)** is an AI-driven full-stack security research company. Learn more at [https://lieling.xyz](https://lieling.xyz)

- 200+ 0day vulnerabilities discovered
- 100% PoC physical verification
- 7-14 day delivery cycle

### Overview

The **Mail Module** is designed for mail server (Exchange, Postfix, Zimbra) vulnerability discovery. It includes specialized sub-agents for protocol fuzzing and logic vulnerability analysis.

### Features

- Protocol state machine fuzzing
- Parsing path heterogeneous analysis
- SMTP/IMAP/POP3 protocol analysis
- Mail-specific vulnerability patterns

### Entry Command

```
/mail
```

### Workflow Agents

| Agent | Purpose |
|-------|---------|
| Mail | Main orchestrator for mail vulnerabilities |
| Code Understander | Analyzes mail server code |
| Target Definer | Defines attack targets |
| Vuln Hunter | Discovers mail vulnerabilities |
| Exploit Builder | Builds mail exploits |
| Validator | Validates findings |
| Variant Analyzer | Finds similar patterns |
| Hypothesis Tester | Tests hypotheses |
| CVE Reference | References related CVEs |
| POC Generator | Generates mail-based POCs |
| Report Generator | Creates vulnerability reports |

### Target Services

- **Microsoft Exchange**
- **Postfix**
- **Zimbra**
- **Dovecot**
- Other SMTP/IMAP/POP3 servers

---

<a id="中文"></a>

## 中文

### 关于猎零 (PreZero)

**猎零 (PreZero)** 是一家 AI 驱动的全栈安全研究公司。了解更多：[https://lieling.xyz](https://lieling.xyz)

- 200+ 已发现 0day
- 100% PoC 物理验证
- 7-14 天交付周期

### 简介

**邮件服务器模块**专为邮件服务器（如 Exchange, Postfix, Zimbra）漏洞挖掘设计。包含协议 fuzz、逻辑漏洞分析等专职 sub-agents。

### 特性

- 协议状态机 Fuzzing
- 解析路径异构分析
- SMTP/IMAP/POP3 协议分析
- 邮件特定漏洞模式

### 入口命令

```
/mail
```

### 工作流 Agents

| Agent | 功能 |
|-------|------|
| Mail | 邮件漏洞主协调器 |
| Code Understander | 分析邮件服务器代码 |
| Target Definer | 定义攻击目标 |
| Vuln Hunter | 发现邮件漏洞 |
| Exploit Builder | 构建邮件利用 |
| Validator | 验证发现 |
| Variant Analyzer | 查找类似模式 |
| Hypothesis Tester | 测试假设 |
| CVE Reference | 参考相关 CVE |
| POC Generator | 生成基于邮件的 POC |
| Report Generator | 创建漏洞报告 |

### 目标服务

- **Microsoft Exchange**
- **Postfix**
- **Zimbra**
- **Dovecot**
- 其他 SMTP/IMAP/POP3 服务器

---

## License / 许可证

Open Source under [lieling.xyz](https://lieling.xyz)
