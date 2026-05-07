# Binary Module / 二进制模块

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

The **Binary Module** is designed for binary program (ELF/PE/Mach-O) vulnerability discovery. It integrates Rizin + rz-ghidra for "binary translation" analysis, combining semantic auditing with Fuzzing orchestration to discover memory safety and logic vulnerabilities.

### Features

- Rizin + Ghidra binary translation
- Static semantic auditing
- Fuzzing harness orchestration
- Memory safety vulnerability detection

### Entry Command

```
/binary
```

### Workflow Agents

| Agent | Purpose |
|-------|---------|
| Binary | Main orchestrator for binary analysis |
| Code Understander | Static translation using Rizin |
| Target Definer | Defines analysis targets |
| Vuln Hunter | Semantic auditing + Fuzz orchestration |
| Exploit Builder | ROP/Shellcode construction |
| Validator | Validates findings |
| Variant Analyzer | Finds similar vulnerabilities |
| Hypothesis Tester | Tests vulnerability hypotheses |
| CVE Reference | Provides heuristic references |
| POC Generator | Generates binary POCs |
| Report Generator | Creates analysis reports |

### Tool Requirements

- **Rizin**: Binary analysis framework
- **rz-ghidra**: Ghidra decompiler integration
- **Frida/GDB**: Dynamic analysis
- **PwnTools**: Exploit development

---

<a id="中文"></a>

## 中文

### 关于猎零 (PreZero)

**猎零 (PreZero)** 是一家 AI 驱动的全栈安全研究公司。了解更多：[https://lieling.xyz](https://lieling.xyz)

- 200+ 已发现 0day
- 100% PoC 物理验证
- 7-14 天交付周期

### 简介

**二进制模块**专为二进制程序（ELF/PE/Mach-O）漏洞挖掘设计。集成 Rizin + rz-ghidra 实现"二进制转译"分析，通过语义审计与 Fuzzing 编排挖掘内存安全与逻辑漏洞。

### 特性

- Rizin + Ghidra 二进制转译
- 静态语义审计
- Fuzzing Harness 编排
- 内存安全漏洞检测

### 入口命令

```
/binary
```

### 工作流 Agents

| Agent | 功能 |
|-------|------|
| Binary | 二进制分析主协调器 |
| Code Understander | 使用 Rizin 进行静态转译 |
| Target Definer | 定义分析目标 |
| Vuln Hunter | 语义审计 + Fuzz 编排 |
| Exploit Builder | ROP/Shellcode 构建 |
| Validator | 验证发现 |
| Variant Analyzer | 查找类似漏洞 |
| Hypothesis Tester | 测试漏洞假设 |
| CVE Reference | 提供启发式参考 |
| POC Generator | 生成二进制 POC |
| Report Generator | 创建分析报告 |

### 工具要求

- **Rizin**: 二进制分析框架
- **rz-ghidra**: Ghidra 反编译器集成
- **Frida/GDB**: 动态分析
- **PwnTools**: 利用开发

---

## License / 许可证

Open Source under [lieling.xyz](https://lieling.xyz)
