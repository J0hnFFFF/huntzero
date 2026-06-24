# zdll CLI UI/UX 设计

> 本文档描述 zdll 的命令行输出风格与交互设计。桌面客户端已移除，所有交互通过 CLI 完成。

## 1. 设计原则

- **日志即界面**：分析过程是一段可滚动、可回溯的彩色事件流。
- **状态可见**：把 Cerebrum 内部事件实时翻译成人类可读的日志消息。
- **减少干扰**：默认全自动；需要用户确认时通过配置控制（`auto_approve`）。
- **CI 友好**：提供 `--quiet`、`--no-color`、SARIF 输出、退出码约定，便于嵌入流水线。
- **一致的事件语义**：同一事件在 CLI 中以统一的图标/颜色呈现。

## 2. 视觉风格

### 2.1 配色

| 用途 | ANSI 颜色 |
|---|---|
| 品牌/标题 | Cyan + Bold |
| 进度/系统 | Cyan |
| Hypothesis | Magenta |
| Task/Drone | White/Faint |
| Finding / Warning | Yellow |
| Error / Critical | Red |
| Success | Green + Bold |
| 次要信息 | White/Faint |

Severity 颜色映射：

| Severity | 颜色 |
|---|---|
| Critical | Red |
| High | Yellow/Orange |
| Medium | Yellow |
| Low | Green |
| None / Info | White/Faint |

### 2.2 图标约定

| 事件 | 图标 |
|---|---|
| 分析开始/系统 | `───` / `┌───┐` banner |
| Round 进度 | `🧠` |
| Hypothesis | `🧬` |
| Drone 任务 | `🚁` |
| Finding | `🟠` |
| 完成 | `✅` / `✔` |
| 错误 | `✖` / `ERROR:` |

## 3. CLI 事件映射

Cerebrum 发布的事件被映射为以下日志行：

| 事件 | 输出示例 |
|---|---|
| `cerebrum_started` | `─── Analysis started ───` |
| `cerebrum_round_started` | `🧠 Round 3/30 [====>..................] | drones:2 | H:4 F:1 T:6 | 02:14` |
| `hypothesis_generated` | `🧬 H-A1B2C3 [85%] Potential SQL injection in auth.go` |
| `hypothesis_updated` | `🧬 H-A1B2C3 → confirmed` |
| `task_queued` | `🚁 T-D4E5F6 queued (code-reviewer)` |
| `drone_failed` | `✖ Drone failed: <error>` |
| `finding_confirmed` | `🟠 FINDING [HIGH] SQL injection in login` |
| `cerebrum_complete` | `✅ Analysis complete` |
| `error` | `ERROR: <message>` |

## 4. 启动 Banner

```text
┌─────────────────────────────────────────┐
│  HIVE-MIND INTEL ENGINE                 │
│  Autonomous · LLM-Dominant · Headless   │
└─────────────────────────────────────────┘
  Mode:      UNATTENDED
  Action:    SCAN
  Workspace: C:\Users\...\zdll_workspace
  Workers:   5
  Model:     kimi-for-coding
```

## 5. 结束摘要

### 5.1 普通模式

```text
✔ Done in 02:14
   Hypotheses: 12  Findings: 3  Tasks: 24
   Reports: C:\Users\...\zdll_workspace\<target>\reports
```

### 5.2 CI 模式

```text
== zdll CI Summary ==
Target:  https://github.com/owner/repo
Elapsed: 2m14s
Diff-base: 7 changed file(s)
Findings: total=3 critical=0 high=1 medium=2 low=0 none=0
By type: dependency_vuln=1 zero_day=2 semantic_signal=0
Fail-on threshold (high): FAIL (1 finding(s))
Baseline (baseline.txt): PASS (0 new, 0 resolved)
```

## 6. 进度条

```text
[====>                  ]
```

- `=` 已完成部分
- `>` 当前进度指针
- ` ` 未完成部分

## 7. 静默与无颜色模式

- `--quiet`：只输出错误和 CI 摘要，适合流水线中只关注结果。
- `--no-color`：禁用 ANSI 颜色；同时尊重 `NO_COLOR` 环境变量。

## 8. 报告输出

CLI 支持以下报告格式：

| 格式 | 扩展名 | 用途 |
|---|---|---|
| Markdown | `.md` | 人工阅读 |
| JSON | `.json` | 机器处理 |
| SARIF | `.sarif.json` | CI / GitHub Advanced Security |
| DOT | `.dot` | Graphviz 可视化 |
| GraphML | `.graphml` | 图分析工具 |

示例：

```powershell
zdll scan ./project -f sarif -o result.sarif.json
zdll report <target> --format json --output report.json
```

## 9. 退出码

| 退出码 | 含义 |
|---|---|
| `0` | 成功 / 策略通过 |
| `1` | 配置或运行时错误 |
| `2` | 策略失败（`--fail-on`）或基线漂移（`--baseline`） |
