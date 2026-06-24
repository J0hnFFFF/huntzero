# zdll 架构设计（解耦版）

## 1. 设计目标

- **无损移植** Python 版核心分析能力（Skills、.bots.md、Agent Session）。
- **双入口共享核心**：CLI 与 Wails 桌面共用同一套 `internal/core`，不重复实现。
- **无网络服务层**：去掉 HTTP/MCP/Redis/Web，只做本地单机分析。
- **解耦优先**：核心引擎只依赖接口，CLI/Wails/存储/LLM/报告均可替换。
- **Skills 原样复用**：沿用现有 `skills/<domain>/SKILL.md` 结构。
- **可维护、可迭代**：新增 Scanner、Report 格式、LLM Provider 只需实现对应接口。

---

## 2. 技术栈

| 层级 | 选型 | 说明 |
|---|---|---|
| 语言 | Go 1.23+ | 官方 Kimi SDK 支持、静态编译 |
| CLI | Cobra | 子命令、参数、配置 |
| 桌面 | Wails v2 + Vue 3 + TypeScript + Vite | 单二进制、事件驱动 |
| 终端输出 | 普通彩色日志（无 TUI） | 参考 `INTERACTION_DESIGN.md` 第 5 节 |
| LLM SDK | `github.com/MoonshotAI/kimi-agent-sdk/go` | 通过 Adapter 接入，可被替换 |
| 代码解析 | `github.com/tree-sitter/go-tree-sitter` + grammar | 通过 Adapter 接入 |
| 配置 | YAML (`~/.config/zdll/config.yaml`) + 环境变量 + 命令行 | 三层覆盖 |
| 持久化 | JSON (`.blackboard.json`) | 兼容原工程格式 |
| 事件 | 内存事件总线 | CLI 与 Wails 各自订阅 |
| 打包 | go build / wails build | Windows `.exe` / macOS `.app` / Linux AppImage |

---

## 3. 解耦分层

```
┌─────────────────────────────────────────────────────────────┐
│  Delivery Layer                                              │
│  cmd/zdll (CLI)          cmd/zdll-desktop (Wails)           │
└──────────────────┬──────────────────────┬───────────────────┘
                   │                      │
                   ▼                      ▼
┌─────────────────────────────────────────────────────────────┐
│  Application Layer                                           │
│  internal/app                                                │
│  - App.Scan / Resume / Stop / Findings / Report / Settings   │
└──────────────────┬───────────────────────────────────────────┘
                   │
                   ▼
┌─────────────────────────────────────────────────────────────┐
│  Core Engine Layer                                           │
│  internal/core                                               │
│  - Engine (Cerebrum)                                         │
│  - Blackboard                                                │
│  - Hypothesis / Finding / Task / Sector 类型                 │
│  - 只依赖 interface，不依赖具体实现                            │
└──────────────────┬───────────────────────────────────────────┘
                   │
        ┌──────────┼──────────┬──────────────┬──────────────┐
        ▼          ▼          ▼              ▼              ▼
┌──────────┐ ┌──────────┐ ┌────────┐ ┌────────────┐ ┌──────────┐
│ internal │ │ internal │ │internal│ │  internal  │ │ internal │
│   /llm   │ │  /store  │ │/report │ │ /eventbus  │ │ /scanner │
│  Adapter │ │  Adapter │ │Adapter │ │   Local    │ │ Adapters │
└──────────┘ └──────────┘ └────────┘ └────────────┘ └──────────┘
        │          │          │              │              │
        ▼          ▼          ▼              ▼              ▼
┌─────────────────────────────────────────────────────────────┐
│  External / Infrastructure                                   │
│  Kimi SDK · Filesystem · osv-scanner · tree-sitter · Wails   │
└─────────────────────────────────────────────────────────────┘
```

核心原则：**`internal/core` 不引用 `cmd/`、`desktop/`、Wails、具体 SDK、文件路径或环境变量。**

---

## 4. 目录结构

```
zdll/
├── cmd/
│   ├── zdll/                    # CLI 入口
│   └── zdll-desktop/            # Wails 桌面入口
├── internal/
│   ├── app/                     # 应用服务层（CLI 与桌面共用）
│   │   └── app.go
│   ├── core/                    # 引擎核心（纯业务逻辑）
│   │   ├── types.go             # Hypothesis / Finding / Task / Sector
│   │   ├── blackboard.go        # 状态、去重、CRUD
│   │   ├── engine.go            # Cerebrum 主循环
│   │   ├── drone_pool.go        # Drone 任务调度接口
│   │   ├── planner.go           # Sector 规划接口
│   │   └── dedup.go             # Bloom + 五层去重
│   ├── event/                   # 事件类型
│   │   └── event.go
│   ├── eventbus/                # 事件总线接口与实现
│   │   ├── bus.go
│   │   └── local.go
│   ├── llm/                     # LLM 抽象 + Kimi Adapter
│   │   ├── client.go            # Client / AgentRunner 接口
│   │   ├── kimi.go              # Kimi SDK Adapter
│   │   ├── config.go
│   │   └── prompts.go
│   ├── skills/                  # Skills 发现与 prompt 构造
│   │   ├── loader.go
│   │   └── skill.go
│   ├── scanner/                 # 扫描器抽象
│   │   ├── scanner.go           # Scanner 接口
│   │   ├── osv.go               # osv-scanner Adapter
│   │   └── semantic.go          # tree-sitter Adapter
│   ├── store/                   # 持久化抽象
│   │   ├── store.go             # Store 接口
│   │   └── json.go              # JSON 文件实现
│   ├── report/                  # 报告生成抽象
│   │   ├── renderer.go          # Renderer 接口
│   │   ├── markdown.go
│   │   ├── json.go
│   │   ├── dot.go
│   │   └── graphml.go
│   ├── config/                  # 配置加载
│   │   └── config.go
│   └── platform/                # 平台相关
│       ├── paths.go
│       ├── tools.go             # osv-scanner 下载/嵌入
│       └── sandbox.go
├── desktop/
│   ├── app.go                   # Wails 绑定实现
│   ├── wails.json
│   └── frontend/                # Vue 3 前端
│       └── src/
├── skills/                      # 原样复用
├── assets/bin/                  # 构建时嵌入的 osv-scanner
├── docs/
│   ├── ARCHITECTURE.md
│   ├── UI_UX.md
│   └── INTERACTION_DESIGN.md
├── .bots.md
├── go.mod
└── README.md
```

---

## 5. 接口定义（核心解耦点）

### 5.1 LLM 抽象（`internal/llm/client.go`）

```go
package llm

type AgentConfig struct {
    Role         string
    AgentFile    string   // --agent-file
    SkillsDir    string   // --skills-dir
    WorkDir      string
    AutoApprove  bool
    ExtraArgs    []string
}

type AgentRunner interface {
    // Run 执行一次 Agent 任务，返回事件流（文本、工具调用、结果等）
    Run(ctx context.Context, cfg AgentConfig, task string) (<-chan Event, error)
}

type Event struct {
    Type    string // text / tool_call / tool_result / error / done
    Content string
}
```

- `internal/llm/kimi.go` 实现 `AgentRunner`，调用官方 SDK。
- 后续支持 OpenAI / Anthropic 只需新增 Adapter。

### 5.2 事件总线（`internal/eventbus/bus.go`）

```go
package eventbus

import "zdll/internal/event"

type Bus interface {
    Publish(e event.Event)
    Subscribe() <-chan event.Event
    Unsubscribe(ch <-chan event.Event)
}
```

- `internal/eventbus/local.go` 提供基于 channel 的内存实现。
- Wails 侧在 `cmd/zdll-desktop` 中订阅该 Bus，并调用 `runtime.EventsEmit`。
- CLI 侧在 `cmd/zdll` 中订阅并打印彩色日志。

### 5.3 持久化（`internal/store/store.go`）

```go
package store

import "zdll/internal/core"

type BlackboardStore interface {
    Save(bb *core.Blackboard) error
    Load(workspaceID string) (*core.Blackboard, error)
    List() ([]WorkspaceMeta, error)
}

type WorkspaceMeta struct {
    ID        string
    Target    string
    Status    string
    UpdatedAt time.Time
}
```

- `internal/store/json.go` 实现 JSON 文件持久化。
- 后续可替换为 SQLite / Badger 而不改 core。

### 5.4 报告渲染（`internal/report/renderer.go`）

```go
package report

import "zdll/internal/core"

type Renderer interface {
    Name() string               // "markdown" / "json" / "dot" / "graphml"
    Render(bb *core.Blackboard) ([]byte, error)
    Extension() string
}
```

- 每个格式一个文件，统一注册到 `cmd` 的 ReportService。
- 新增报告格式只需实现 `Renderer` 并注册。

### 5.5 Scanner 抽象（`internal/scanner/scanner.go`）

```go
package scanner

type Scanner interface {
    Name() string
    Scan(ctx context.Context, target string) (Findings, error)
}

type Finding struct {
    Source  string // "osv" / "semantic"
    Title   string
    Severity string
    Evidence string
}
```

- `internal/scanner/osv.go` 调用 osv-scanner 二进制。
- `internal/scanner/semantic.go` 调用 tree-sitter。
- Drones 通过 `Scanner` 接口按需调用，不直接依赖外部工具。

### 5.6 Drone 池（`internal/core/drone_pool.go`）

```go
package core

type DronePool interface {
    Submit(ctx context.Context, t Task) error
    SetCapacity(n int)
    Wait()
}
```

- 实现基于 `golang.org/x/sync/semaphore` 或 worker channel。
- Core 只关心“提交任务并等待”，不关心并发原语。

---

## 6. 核心流程

### 6.1 启动一次分析

```
[CLI / Wails]
    │
    ▼
[app.App.Scan(target, opts)]
    │
    ├── 解析 target（本地 / Git）
    ├── 创建/加载 Workspace
    ├── 加载 Blackboard（resume）
    ├── 初始化 Engine（注入 Store / Bus / LLM / Scanners / DronePool）
    └── 启动 Engine
              │
              ▼
        [core.Engine.Run(ctx)]
              │
              ├── 发布 cerebrum_started
              ├── 多轮循环
              │     ├── 生成/更新 Hypothesis
              │     ├── 去重
              │     ├── 规划 Sector（大项目）
              │     ├── 派发 Task → DronePool
              │     ├── Drone 调用 LLM Agent / Scanner
              │     ├── Critic 审查
              │     └── 晋升 Finding
              ├── 发布 cerebrum_complete
              └── 调用 report.Renderer 生成报告
```

### 6.2 事件流

`internal/core` 在关键节点发布事件：

| 事件 | 发布时机 | 前端表现 |
|---|---|---|
| `cerebrum_started` | Engine.Run 开始 | 系统消息 + 进度卡片 |
| `round_started` | 每轮开始 | 进度卡片更新 |
| `hypothesis_generated` | 生成新假设 | Hypothesis 卡片 |
| `hypothesis_updated` | 状态/置信度变化 | 更新已有卡片 |
| `task_queued` | Task 进入队列 | Hypothesis 任务数 +1 |
| `drone_launched` | 任务开始执行 | 活跃 Drone 数 +1 |
| `drone_completed` | 任务完成 | 活跃 Drone 数 -1，可能带回证据 |
| `finding_confirmed` | 假设晋升为 Finding | Finding 卡片（高亮） |
| `cerebrum_paused` | 收到 pause 信号 | 进度卡片显示暂停 |
| `cerebrum_stopped` | 收到 stop 信号 | 系统消息 + 保存状态 |
| `cerebrum_complete` | 全部结束 | 摘要卡片 + 导出按钮 |

所有事件通过 `eventbus.Bus` 分发，**core 不直接操作 UI 或日志**。

### 6.3 Pause / Resume

- Engine 接收 `context.Context`。
- 用户点击 Pause 或执行 `/pause`：外部取消 ctx，Engine 在检查点保存 Blackboard 并退出。
- Resume：App 重新加载 Blackboard，恢复 `Round` 计数和未完成的 Hypotheses，继续下一轮。
- 当前正在执行的 Drone 任务结果可能丢失，设计为可重入：Task 完成后若 Blackboard 已不存在则丢弃。

---

## 7. 模块映射（Python → Go）

| 原 Python | 新 Go 包 | 备注 |
|---|---|---|
| `kimi.py` / `kimi_hive.py` | `cmd/zdll/`, `internal/app` | 命令路由 + 应用服务 |
| `engine/blackboard.py` | `internal/core/blackboard.go` | 状态与去重 |
| `engine/cerebrum.py` | `internal/core/engine.go` | 主循环 |
| `engine/drone.py` | `internal/core/drone_pool.go` + `internal/llm` | 任务调度 + Agent 执行 |
| `engine/sector.py` | `internal/core/planner.go` | Sector 规划接口 |
| `engine/session_pool.py` | `internal/llm/kimi.go` | 由 SDK 管理或内部池 |
| `engine/semantic_helper.py` | `internal/scanner/semantic.go` | AST 扫描器 |
| `engine/osv_bridge.py` | `internal/scanner/osv.go` | OSV 扫描器 |
| `tools/exploit_analyzer.py` | `internal/scanner/exploit.go` | 利用前提分析 |
| `server/*` | 不移植 | 去掉网络服务层 |
| `engine/fuzzer.py` | 不移植 | 本次去掉 |
| `tools/poc_sandbox.py` | 不移植 | 本次去掉 |

---

## 8. 配置体系

优先级（高 → 低）：

1. 命令行参数
2. 环境变量（`ZDLL_API_KEY`、`ZDLL_WORKSPACE`）
3. `~/.config/zdll/config.yaml`
4. 内置默认值

示例：

```yaml
llm:
  provider: kimi
  api_key: sk-...
  base_url: https://api.moonshot.ai/v1
  model: kimi-k2-thinking-turbo
  thinking: true

analysis:
  workers: 5
  max_rounds: 30
  max_tasks: 200
  max_time: 7200
  stagnation: 3
  auto_approve: true
  sector_threshold_files: 5000

paths:
  workspace: ~/zdll_workspace
  skills: ./skills
  osv_scanner: ""   # 空则使用嵌入/自动下载
```

---

## 9. Skills 机制

- **不改动** `skills/` 目录结构与 Markdown。
- `internal/skills/loader.go` 读取每个 `SKILL.md` 的 frontmatter（name、description）与 `references/*.md` 正文。
- Cerebrum/Drone 构造 prompt 时引用 Skill 内容。
- Agent 运行时通过 `kimi.WithSkillsDir(skillsDir)` 和 `kimi.WithArgs("--agent-file", path)` 透传，**保证 prompt 注入行为与 Python 版一致**。

---

## 10. osv-scanner 与 tree-sitter 打包策略

### 10.1 osv-scanner

1. 构建时把对应平台的 `osv-scanner` 放入 `assets/bin/osv-scanner-<os>-<arch>`。
2. 使用 `//go:embed` 嵌入。
3. 运行时释放到 `~/.cache/zdll/bin/osv-scanner` 并执行。
4. 若未嵌入，尝试从 GitHub Release 自动下载。

### 10.2 tree-sitter

- Go binding `go-tree-sitter` 作为 Go module 依赖。
- 各语言 grammar 作为子 module 或预编译 `.so`/`.wasm` 在运行时加载。
- 缺失语言自动降级为 regex 扫描，并在事件中提示。

---

## 11. 持久化与 Resume

- 每个 workspace 保存 `.blackboard.json`。
- 保存采用原子写：`.blackboard.json.tmp` → `rename`。
- Blackboard 结构包含：
  - `ID`、`Target`、`Status`
  - `Round`、`MaxRounds`
  - `Hypotheses` 列表
  - `Findings` 列表
  - `Tasks` 历史
  - `Sectors` 列表
- Resume 时恢复 Blackboard，Engine 继续下一轮；未完成的 Tasks 会被重新生成。

---

## 12. CLI 与 Wails 的复用

### 12.1 CLI（`cmd/zdll`）

```go
func main() {
    cfg := config.Load()
    bus := eventbus.NewLocal()
    store := jsonstore.New(cfg.Workspace)
    llm := llm.NewKimi(cfg.LLM)
    scanners := scanner.All(cfg)
    app := app.New(cfg, bus, store, llm, scanners)

    // 订阅事件并打印彩色日志
    go cliLogger(bus.Subscribe())

    cobra.Execute()
}
```

### 12.2 Wails（`cmd/zdll-desktop`）

```go
func main() {
    cfg := config.Load()
    bus := eventbus.NewLocal()
    store := jsonstore.New(cfg.Workspace)
    llm := llm.NewKimi(cfg.LLM)
    scanners := scanner.All(cfg)
    app := app.New(cfg, bus, store, llm, scanners)

    err := wails.Run(&options.App{
        Title:  "zdll",
        Width:  1280,
        Height: 800,
        Bind: []interface{}{
            desktop.NewAPI(app),
        },
        OnStartup: func(ctx context.Context) {
            desktop.StartEventForwarder(ctx, bus)
        },
    })
}
```

- `desktop.StartEventForwarder` 把 `eventbus.Bus` 的事件转成 Wails `runtime.EventsEmit`。
- 前端通过 `EventsOn` 订阅，更新 Vue 状态。

---

## 13. 实现阶段

| 阶段 | 内容 | 产出 |
|---|---|---|
| 0 | 骨架 + 配置 + 事件总线 | go.mod、目录、config、Bus |
| 1 | 核心类型 + Blackboard + Store | 持久化、resume |
| 2 | LLM 接口 + Kimi Adapter | 能跑单个 Agent 任务 |
| 3 | DronePool + 单任务执行 | 并发调度 |
| 4 | Cerebrum 主循环 | 生成假设 → 派发 → 审查 |
| 5 | Report Renderer | Markdown / JSON |
| 6 | CLI 命令 | scan / resume / report / config |
| 7 | Scanner Adapters | osv-scanner / semantic |
| 8 | Sector Planner | 大项目分区 |
| 9 | Wails Desktop | 聊天流 + 详情抽屉 |
| 10 | 打包 + osv 嵌入 | 各平台安装包 |

---

## 14. 关键决策

1. **纯接口驱动**：core 只依赖 `llm.AgentRunner`、`store.BlackboardStore`、`eventbus.Bus`、`scanner.Scanner`、`report.Renderer` 等接口。
2. **单一 App 服务**：`internal/app` 封装用例，CLI 与 Wails 都通过它操作核心。
3. **事件总线解耦 UI**：core 发布事件，不关心前端框架或日志实现。
4. **CLI 不做 TUI**：普通彩色日志，降低复杂度，输出更稳定。
5. **配置与 workspace 分离**：配置在 `~/.config/zdll/`，workspace 在 `~/zdll_workspace/`。
6. **先单机、后联网**：后续若需 team server，只需在 `internal/app` 上加网络适配层，core 不变。
