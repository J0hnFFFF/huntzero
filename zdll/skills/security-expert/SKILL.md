---
description: 路由侦察情报简报 (V7 Domain Intelligence Brief) — 已整合入 Cerebrum，本文件作为辅助地形参考
---

# 目标侦察与领域路由 (Security Recon Brief)

> [!NOTE]
> V7 中路由决策由 Cerebrum 主脑自主完成。
> 本文件提供**通用的初始侦察地形知识**，而非路由规则。

## 初始侦察的目标

当面对一个未知目标时，分析的第一步是建立**架构指纹**：
明确目标的主要暴露面是什么，以便选择最相关的领域地形知识作为分析上下文。

## 通用侦察观察点

### 1. 文件树测绘（理解项目边界）
- 入口文件（`main.go`, `index.php`, `app.py`, `Makefile`, `package.json`）
- 配置文件（暴露端口、服务依赖、中间件配置）
- 二进制文件（ELF/PE/固件——表明需要逆向分析）

### 2. 业务归属判定（基于实际暴露面，而非编程语言）

| 暴露面特征 | 对应领域知识 |
|-----------|------------|
| HTTP/HTTPS API、Web 界面、REST/GraphQL | `web` 地形情报 |
| 包含浏览器引擎代码（V8/WebKit/DOM）| `browser` 地形情报 |
| 编译好的 ELF/PE 二进制、固件 | `binary` 地形情报 |
| 邮件协议栈（SMTP/IMAP/MIME 解析）| `mail` 地形情报 |
| 客户端工具、安全扫描器、管理端 | `counter` 地形情报 |
| LLM/Agent 框架、MCP Server、Skill/Plugin 系统 | `ai-agent` 地形情报 |
| 包管理器、构建管道、发布流水线 | `supply-chain` 地形情报 |
| KV 存储、SQL 数据库、LSM-Tree 引擎 | `storage-engine` 地形情报 |
| JSON/XML/Protobuf/图片等格式解析 | `data-parser` 地形情报 |
| Electron/Tauri/CEF 桌面应用 | `desktop` 地形情报 |
| K8s Operator/Helm/Terraform/CI-CD | `infra` 地形情报 |
| Go/Rust/C++ 通用工具库、数据结构库 | `foundation-lib` 地形情报 |

> **关键原则**：不要依赖编程语言扩展名判断领域。Go 写的可以是 Web API，也可以是命令行工具。C++ 写的可以是 Web 服务器，也可以是浏览器引擎。**看暴露面，不看语法。** 同时注意：许多项目**跨越多个领域**（如 AI Agent 同时也是 Web 应用），需要同时加载多个地形情报。

### 3. 安全历史上下文
- 搜索目标代码中的 `security`, `cve`, `fix`, `vuln` 等关键词的提交注释或代码注释
- 这些痕迹往往是开发者自己标注的高风险区域，是假设生成的高价值起点

## 初始发现快速落盘

侦察过程中若发现以下内容，**立即写入 `.audit_notes.md`**：
- 硬编码的 API 密钥、密码、Token
- 明显的危险调用（`eval(user_input)`, `system($_GET[...])`, `exec(req.query...)`)
- 暴露在外的内部服务地址或数据库连接字符串
