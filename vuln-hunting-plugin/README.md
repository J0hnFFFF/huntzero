# Vuln Hunting Toolkit / 漏洞挖掘工具包

[English](#english) | [中文](#中文)

---

<a id="english"></a>

## English

### About PreZero (猎零)

**Vuln Hunting Toolkit** is an open source project by [PreZero (猎零)](https://lieling.xyz) - an AI-driven full-stack security research company.

**Core Capabilities:**
- **0day Vulnerability Research** - Discover unknown vulnerabilities in open source components, middleware, and core business code
- **AI Red Team** - Simulate APT attack chains with 7×24 automated BAS (Breach and Attack Simulation)
- **Crawfish** - Next-gen AI-powered security validation platform
- **Open Source Projects** - Share core security tools and vulnerability research resources

**Key Metrics:**
- 200+ 0day vulnerabilities discovered
- 100% PoC physical verification
- 7-14 day delivery cycle

**Technology:**
Hive-Mind AI Engine - Endows machines with expert-level hacker thinking
- Global semantic code understanding
- Multi-step attack path deduction
- Autonomous POC validation

---

### Overview

**Vuln Hunting Toolkit** is an Agentic AI-based vulnerability discovery framework that focuses on imbuing Large Language Models with **human expert mental models**.

Unlike traditional vulnerability scanners, this toolkit acts as a **domain-aware security researcher** capable of understanding context, generating hypotheses, and systematically hunting for vulnerabilities.

### Core Philosophy

- **Thought > Tools**: The core is not running tools, but simulating the researcher's thought process (Chain of Thought).
- **Inspire > Restrict**: Historical CVEs and pattern libraries serve as inspiration, not checklists, to preserve LLM's generalization ability.
- **Translate > Read**: For binaries, use tools to translate unreadable bytecode into LLM-understandable intermediate representations (Pseudocode/IR).

### Modules

| Module | Target | Core Strategy | Entry Command |
|--------|--------|---------------|---------------|
| **Counter** | Honeypots, General Software | Full-process automation, broad attack surface coverage | `/counter` |
| **Mail** | SMTP/IMAP/POP3 Services | Protocol state machine fuzzing, parsing path heterogeneous analysis | `/mail` |
| **Browser** | V8/WebKit/Spidermonkey | JIT optimization pipeline audit, object lifecycle management | `/browser` |
| **Web** | Web Applications, Business Logic | Trust boundary tracking, business logic isomorphism analysis | `/web` |
| **Binary** | EXE/ELF/Mach-O Files | Rizin + Ghidra translation, static semantic auditing, Harness orchestration | `/binary` |

### Quick Start

```bash
# Choose your target module
cd vuln-hunting-plugin/Counter  # or Web, Browser, Mail, Binary

# Start the Agent
/entry_command

# The Agent will follow defined Chain of Thought workflows
```

### Tool Integration

- **Rizin / rz-ghidra**: Binary decompilation and static analysis
- **Frida / GDB**: Dynamic verification and hypothesis testing
- **Python (Requests/PwnTools)**: POC and exploit script generation

### Project Structure

```
vuln-hunting-plugin/
├── Counter/              # Honeypot countermeasures module
├── Mail/                 # Mail server vulnerability hunting
├── Browser/              # Browser engine vulnerability hunting
├── Web/                  # Web application vulnerability hunting
├── Binary/               # Binary program vulnerability hunting
│   └── .agent/workflows/ # Agent workflow definitions
└── README.md
```

### Disclaimer

This toolkit is for **authorized security research and testing only**. Do not use for illegal activities.

---

<a id="中文"></a>

## 中文

### 关于猎零 (PreZero)

**漏洞挖掘工具包 (Vuln Hunting Toolkit)** 是由 [PreZero (猎零)](https://lieling.xyz) 开源的安全研究项目。

**核心业务：**
- **漏洞研究** - 覆盖开源组件、中间件及核心业务代码，用第一性原理推演逻辑缺陷，挖掘未知 0day 漏洞
- **AI红队** - 模拟 APT 组织完整攻击链路，7×24 小时自动化 BAS 攻击模拟
- **Crawfish** - 新一代 AI 驱动安全产品，自动化攻防验证
- **开源项目** - 开放核心安全工具链与漏洞研究资源，赋能社区

**核心数据：**
- 200+ 已发现 0day
- 100% PoC 物理验证，可复现才交付
- 7-14 天交付周期

**技术引擎：**
Hive-Mind AI 引擎 - 赋予机器与人类专家同等的黑客思维
- 代码全局语义理解
- 多步攻击路径推演
- 无害化 POC 自动构造

---

### 简介

**漏洞挖掘工具包 (Vuln Hunting Toolkit)** 是一套基于 Agentic AI 的漏洞挖掘框架，专注于将**人类专家的思维模型 (Mental Models)** 赋予大语言模型。

不再是死板的漏洞扫描器，而是具备**领域感知能力**的安全研究员。

### 核心理念

- **思想 > 工具**: 核心不是运行工具，而是模拟研究员的思考路径 (Chain of Thought)。
- **启发 > 限制**: 历史 CVE 和模式库用于启发灵感，而非作为检查清单，以保护 LLM 的泛化推理能力。
- **转译 > 阅读**: 针对二进制，通过工具将不可读的字节流转译为 LLM 可理解的中间表示 (Pseudocode/IR)。

### 模块矩阵

| 模块 | 适用场景 | 核心策略 | 入口命令 |
|:-----|:---------|:---------|:---------|
| **反制 (Counter)** | 蜜罐反制、通用软件 | 全流程自动化、攻击面广泛覆盖 | `/counter` |
| **邮服 (Mail)** | SMTP/IMAP/POP3 服务 | 协议状态机 Fuzzing、解析路径异构分析 | `/mail` |
| **浏览器 (Browser)** | V8/WebKit/Spidermonkey | JIT 优化管道审计、对象生命周期管理 | `/browser` |
| **Web** | Web 应用、业务逻辑 | 信任边界追踪、业务逻辑同构分析 | `/web` |
| **二进制 (Binary)** | EXE/ELF/Mach-O 文件 | Rizin + Ghidra 转译、静态语义审计、Harness 编排 | `/binary` |

### 快速开始

```bash
# 选择目标模块目录
cd vuln-hunting-plugin/反制  # 或 Web, 浏览器, 邮服, 二进制

# 启动 Agent
/入口命令

# Agent 将按照定义的思维链进行工作
```

### 工具集成

- **Rizin / rz-ghidra**: 二进制反编译与静态分析
- **Frida / GDB**: 动态验证与假设测试
- **Python (Requests/PwnTools)**: POC 和利用脚本生成

### 目录结构

```
vuln-hunting-plugin/
├── 反制/                  # 蜜罐反制模块
├── 邮服/                  # 邮件服务器漏洞挖掘
├── 浏览器/                # 浏览器引擎漏洞挖掘
├── Web/                   # Web 应用漏洞挖掘
├── 二进制/                # 二进制程序漏洞挖掘
│   └── .agent/workflows/ # Agent 工作流定义
└── README.md
```

### 思维模型示例

**二进制挖掘示例**:
> **Agent 思考**: "目标开启了 NX 保护，无法直接执行栈。我发现了一个栈溢出，但我需要构建 ROP 链..."

**Web 逻辑挖掘示例**:
> **Agent 思考**: "这是支付接口。虽然参数签名了，但我注意到 `update_order` 和 `create_order` 共享了同一套鉴权逻辑..."

### 免责声明

本工具包仅供**安全研究与授权测试**使用。请勿用于非法用途。

---

## License / 许可证

Open Source under [lieling.xyz](https://lieling.xyz)

本项目由 [猎零 PreZero](https://lieling.xyz) 开源。
