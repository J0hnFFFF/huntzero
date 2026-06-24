# zdll — Zero-Day Local LLM Hunter

> Go 重制的 kimiSec/Hive-Mind 单机 CLI 版。
> 保留核心分析能力，去掉 Web/HTTP/MCP/Redis/JSON-RPC、Fuzzer、PoC Sandbox 与桌面客户端，专注无头 CLI 与 CI 集成。

## 特性

- 🧠 第一性原理驱动的 LLM 安全分析引擎
- 🔍 Sector-Based 大型项目分区分析
- 🚁 Drone 并发任务池 + Session 预热
- 🧬 五层假设去重 + Bloom 过滤器
- 📦 自带 osv-scanner，开箱即用
- 🖥️ 纯 CLI，适合本地与 CI 流水线
- 💾 断点续传：随时 Stop，随时 Resume
- 📊 SARIF 输出、基线对比、变更范围扫描

## 目录

- `docs/ARCHITECTURE.md` — 架构设计
- `docs/INTERACTION_DESIGN.md` — 事件与交互设计
- `docs/UI_UX.md` — CLI 输出风格参考
- `internal/core/` — 分析引擎核心（engine、blackboard、drone、tree-sitter）
- `internal/llm/` — Kimi LLM 接入
- `internal/scanner/` — OSV 与语义扫描器
- `internal/report/` — Markdown/JSON/SARIF/DOT/GraphML 渲染
- `internal/app/` — 应用服务层
- `cmd/zdll/` — CLI 入口
- `skills/` — 领域技能库

## 快速开始

```bash
# 设置 Kimi API Key
export KIMI_API_KEY='sk-...'        # Linux/macOS
$env:KIMI_API_KEY='sk-...'          # PowerShell

# 扫描本地项目
zdll scan /path/to/project

# 扫描 Git 仓库
zdll scan https://github.com/owner/repo

# CI 模式：输出 SARIF，命中 high 及以上则退出码 2
zdll ci https://github.com/owner/repo -f sarif -o zdll.sarif.json --fail-on high

# CI 模式：输出 JSON 摘要
zdll ci https://github.com/owner/repo --summary-format json

# 仅扫描相对于 main 分支的变更文件
zdll scan /path/to/project --diff-base main

# 配置检查
zdll config init
zdll config validate
zdll config get llm.model
zdll config set analysis.workers 8
zdll config unset llm.model

# 列出工作区
zdll list

# 从已有工作区生成报告
zdll report https://github.com/owner/repo --format sarif -o report.sarif
```

## 构建

```powershell
# Windows 需要 MinGW GCC（go-tree-sitter 依赖 CGO）
$env:CC='C:\ProgramData\mingw64\mingw64\bin\gcc.exe'
go build -o zdll.exe ./cmd/zdll
```

```bash
# Linux / macOS
go build -o zdll ./cmd/zdll
```

## 测试

```powershell
$env:CC='C:\ProgramData\mingw64\mingw64\bin\gcc.exe'
go test ./...
```

## License

MIT
