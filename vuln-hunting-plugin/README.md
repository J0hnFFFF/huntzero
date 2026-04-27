# 漏洞挖掘自动化工具包 (Vuln Hunting Toolkit)

一套基于 Agentic AI 的漏洞挖掘框架，专注于将**人类专家的思维模型 (Mental Models)** 赋予大模型。

不再是死板的漏洞扫描器，而是具备**领域感知能力**的安全研究员。

## 🌟 核心理念

*   **思想 > 工具**: 核心不是运行工具，而是模拟研究员的思考路径 (Thought Process)。
*   **启发 > 限制**: 历史 CVE 和模式库用于启发灵感，而非作为检查清单，以保护 LLM 的泛化推理能力。
*   **转译 > 阅读**: 针对二进制，通过工具将不可读的字节流转译为 LLM 可理解的中间表示 (Pseudocode/IR)。

## 🧩 模块矩阵 (Modules)

针对不同技术领域，提供深度定制的挖掘工作流：

| 模块 | 适用场景 | 核心策略 | 入口命令 |
| :--- | :--- | :--- | :--- |
| **反制 (Counter)** | 蜜罐反制、通用软件 | 全流程自动化、攻击面广泛覆盖 | `/counter` |
| **邮服 (Mail)** | SMTP/IMAP/POP3 服务 | 协议状态机 Fuzzing、解析路径异构分析 | `/mail`|
| **浏览器 (Browser)**| V8/WebKit/Spidermonkey | JIT 优化管道审计、对象生命周期管理 | `/browser` |
| **Web** | Web 应用、业务逻辑 | 信任边界追踪、业务逻辑同构分析 | `/web`  |
| **二进制 (Binary)** | EXE/ELF 文件分析 | **Rizin+Ghidra 转译**、静态语义审计、Harness 编排 | `/binary` |

*(注：不同模块通过独立的 `manifest.json` 加载，请在对应目录下启动)*

## 📂 目录结构

```
vuln-hunting-plugin/
├── 反制/                  # 通用反制模块
├── 邮服/                  # 邮件服务器专用模块
├── 浏览器/                # 浏览器引擎专用模块
├── Web/                   # Web 业务逻辑专用模块
├── 二进制/                # 二进制程序专用模块 (New!)
│   ├── .agent/workflows/
│   │   ├── binary.md           # [入口] 总控思维链
│   │   ├── code-understander.md # 静态转译 (Rizin)
│   │   ├── vuln-hunter.md       # 语义审计 + Fuzz编排
│   │   ├── exploit-builder.md   # ROP/Shellcode 构建
│   │   ├── cve-reference.md     # 启发式参考
│   │   └── ...
│   └── manifest.json
└── README.md
```

## 🚀 快速开始

### 1. 选择战场
进入对应的目录：
```bash
# 例如：挖掘二进制漏洞
cd vuln-hunting-plugin/二进制
```

### 2. 启动 Agent
在 Agent 环境中运行：
```
/入口命令
```

### 3. 交互与反馈
Agent 会按照定义的思维链（Chain of Thought）进行工作：
1.  **Code Understander**: "我看到这是一段处理用户输入的代码..."
2.  **Hypothesis Tester**: "我猜如果输入超长字符..."
3.  **Vuln Hunter**: "验证成功！发现栈溢出..."

## 🧠 思维模型示例

### 二进制挖掘
> **Agent 思考**: "目标开启了 NX 保护，无法直接执行栈。我发现了一个栈溢出，但我需要构建 ROP 链。首先利用 `puts` 泄露 libc 基址..."

### Web 逻辑挖掘
> **Agent 思考**: "这是支付接口。虽然参数签名了，但我注意到 `update_order` 接口和 `create_order` 共享了同一套鉴权逻辑（同构性），而 `update` 接口似乎漏掉了对 amount 的负数检查..."

## 🛠️ 工具集成

*   **Rizin / rz-ghidra**: 用于二进制反编译与静态分析。
*   **Frida / GDB**: 用于动态验证与假设测试。
*   **Python (Requests/PwnTools)**: 用于生成 POC 和利用脚本。

## ⚠️ 免责声明

本工具包仅供安全研究与授权测试使用。请勿用于非法用途。
