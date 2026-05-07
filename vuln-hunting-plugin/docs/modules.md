# Modules Documentation / 模块文档

[English](#english) | [中文](#中文)

---

<a id="english"></a>

## English

### Module Matrix

| Module | Target | Strategy | Command |
|--------|--------|----------|---------|
| **Counter** | Honeypots, General Software | Full-process automation | `/counter` |
| **Mail** | SMTP/IMAP/POP3 Services | Protocol fuzzing | `/mail` |
| **Browser** | V8/WebKit/Spidermonkey | JIT audit | `/browser` |
| **Web** | Web Applications | Trust boundary tracking | `/web` |
| **Binary** | EXE/ELF/Mach-O | Rizin + Ghidra translation | `/binary` |

### Common Workflow Agents

All modules share a common set of specialized agents:

| Agent | Description |
|-------|-------------|
| **Code Understander** | Analyzes target code/systems |
| **Target Definer** | Identifies attack surface |
| **Vuln Hunter** | Core vulnerability discovery |
| **Exploit Builder** | Constructs exploits/POCs |
| **Validator** | Verifies findings |
| **Variant Analyzer** | Finds similar vulnerabilities |
| **Hypothesis Tester** | Tests vulnerability hypotheses |
| **CVE Reference** | Historical vulnerability context |
| **POC Generator** | Generates proof-of-concept code |
| **Report Generator** | Creates detailed reports |

### Module-Specific Features

#### Counter Module
- Orchestrator agent for coordination
- Safety guard for ethical boundaries
- Hook system for validation

#### Binary Module
- Rizin integration for binary translation
- Ghidra decompiler integration
- ROP chain construction
- Shellcode generation

#### Web Module
- OWASP Top 10 coverage
- Business logic analysis
- Trust boundary tracking

#### Browser Module
- JIT optimization analysis
- Object lifecycle management
- Memory safety auditing

#### Mail Module
- SMTP/IMAP/POP3 protocol fuzzing
- State machine analysis
- Parsing path heterogeneity

---

<a id="中文"></a>

## 中文

### 模块矩阵

| 模块 | 目标 | 策略 | 命令 |
|:-----|:-----|:-----|:-----|
| **反制 (Counter)** | 蜜罐、通用软件 | 全流程自动化 | `/counter` |
| **邮服 (Mail)** | SMTP/IMAP/POP3 服务 | 协议 fuzz | `/mail` |
| **浏览器 (Browser)** | V8/WebKit/Spidermonkey | JIT 审计 | `/browser` |
| **Web** | Web 应用 | 信任边界追踪 | `/web` |
| **二进制 (Binary)** | EXE/ELF/Mach-O | Rizin + Ghidra 转译 | `/binary` |

### 通用工作流 Agents

所有模块共享一套专业 Agents：

| Agent | 描述 |
|-------|------|
| **Code Understander** | 分析目标代码/系统 |
| **Target Definer** | 识别攻击面 |
| **Vuln Hunter** | 核心漏洞发现 |
| **Exploit Builder** | 构建利用/POC |
| **Validator** | 验证发现 |
| **Variant Analyzer** | 查找类似漏洞 |
| **Hypothesis Tester** | 测试漏洞假设 |
| **CVE Reference** | 历史漏洞背景 |
| **POC Generator** | 生成概念验证代码 |
| **Report Generator** | 创建详细报告 |

### 模块特定功能

#### 反制模块
- 协调器 Agent 用于协调
- 安全卫士确保伦理边界
- 钩子系统用于验证

#### 二进制模块
- Rizin 集成用于二进制转译
- Ghidra 反编译器集成
- ROP 链构建
- Shellcode 生成

#### Web 模块
- OWASP Top 10 覆盖
- 业务逻辑分析
- 信任边界追踪

#### 浏览器模块
- JIT 优化分析
- 对象生命周期管理
- 内存安全审计

#### 邮件模块
- SMTP/IMAP/POP3 协议 fuzzing
- 状态机分析
- 解析路径异构性

---

## License / 许可证

Open Source under [lieling.xyz](https://lieling.xyz)
