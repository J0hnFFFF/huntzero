# zdll 交互设计

基于现有 `local_workspace/projects/*/reports/` 的实际产出设计。核心参考：

- `1Panel.git_20260612_114138.{md,json,dot,graphml}` — 0 Finding，但含 6 个 Hypothesis
- `fastjson2.git_20260408_225653.{md,json}` — 5 个 Findings + 56 个 Hypotheses

---

## 1. 核心交互（不可跳过）

| 编号 | 核心交互 | 说明 |
|---|---|---|
| 1 | **启动分析** | 输入本地目录或 Git URL，配置参数，开始扫描 |
| 2 | **实时监测** | 在聊天流中看 Round、Hypothesis、Drone、Finding 的动态 |
| 3 | **暂停/停止/恢复** | 长时任务必须可中断，状态不丢失 |
| 4 | **查看 Finding** |  severity、标题、描述、Critic Note、代码证据 |
| 5 | **查看 Hypothesis** | 状态、置信度、任务数、证据数、关联 Finding |
| 6 | **历史工作区** | 打开历史分析，恢复聊天/状态 |
| 7 | **导出报告** | Markdown / JSON / 图文件 |
| 8 | **配置环境** | API Key、Model、默认参数、Skills、osv-scanner |
| 9 | **命令输入** | 用自然语言或 `/slash` 命令控制 |
| 10 | **结果搜索/过滤** | 按 severity、关键词、状态过滤 Findings/Hypotheses |

> **Skills 不算独立交互**，它在幕后通过 `skills_dir` 注入 Agent，用户只需在 Settings 中查看/切换 Skills 目录。

> 这些交互都由 `internal/app` 统一暴露，CLI 与 Wails 共享同一套后端能力。详见 `ARCHITECTURE.md`。

---

## 2. 数据模型 → 界面映射

### 2.1 Finding（核心结果）

来源于 `report.json` 中 `findings[]`：

```json
{
  "id": "F-C224AD",
  "hypothesis_id": "H-9BDEBF",
  "title": "ObjectReaderProvider.checkAutoType has critical security gaps...",
  "severity": "high",
  "description": "...\n\n> **[Critic Note]**: All three claims are verified...",
  "evidence": "```java\n// ObjectReaderProvider.java lines 68...",
  "created_at": 1775658916.8977246
}
```

**界面展示字段**：

| 字段 | 展示方式 |
|---|---|
| `severity` | 左侧色条 + 徽章（🔴 Critical / 🟠 High / 🟡 Medium / 🔵 Low） |
| `title` | 卡片标题，点击展开 |
| `hypothesis_id` | 可点击跳转对应 Hypothesis |
| `description` | Markdown 渲染，Critic Note 用引用块高亮 |
| `evidence` | 代码块，Shiki 语法高亮，带复制按钮 |
| `created_at` | 相对时间（如 "2 分钟前"） |

### 2.2 Hypothesis（中间推理）

来源于 `report.json` 中 `hypotheses[]`：

```json
{
  "id": "H-9BDEBF",
  "description": "AutoType blacklist in ObjectReaderProvider can be bypassed...",
  "confidence": 1.0,
  "status": "active",
  "parent_id": null,
  "tasks_count": 7,
  "evidence_count": 4
}
```

**界面展示字段**：

| 字段 | 展示方式 |
|---|---|
| `id` | 标签，如 H-9BDEBF |
| `description` | 可折叠摘要 |
| `confidence` | 进度条（0–100%） |
| `status` | 状态点：pending / active / suspected / confirmed / discarded |
| `tasks_count` | 已派发 Drone 任务数 |
| `evidence_count` | 收集证据数 |
| `parent_id` | 如有，显示父子关系 |

### 2.3 Summary（分析摘要）

来源于 `report.json` 中 `summary`：

```json
{
  "total_hypotheses": 56,
  "confirmed": 4,
  "discarded": 37,
  "total_findings": 5,
  "tasks_executed": 38
}
```

**界面展示**：分析结束时的顶部摘要卡片。

### 2.4 Exploit Chain Graph（辅助视图）

来源于 `.dot` / `.graphml`：

当前实现只有 Hypothesis 节点，没有边。作为**可选视图**保留，后续如果生成跨 Sector 利用链再强化。

---

## 3. 用户流程

### 3.1 首次启动

1. 打开应用 → 暗色欢迎界面。
2. 底部输入框提示：`Enter a Git URL or local path to scan...`
3. 若未配置 API Key，自动弹出 Settings 抽屉。

### 3.2 开始一次分析

用户输入：

```text
scan https://github.com/alibaba/fastjson2.git --workers 8
```

或拖拽本地文件夹到输入框。

系统响应：

1. 在聊天流中显示用户消息气泡。
2. 显示 "Analysis started" 系统消息。
3. 显示 **进度卡片**：目标、Round、Drones、Findings、已用时间、预估剩余。
4. 随着事件到达，动态插入：
   - Hypothesis 卡片
   - Drone 完成提示
   - Finding 卡片（高亮）
5. 分析结束时显示 **摘要卡片**：
   ```
   ✅ Analysis complete
   56 hypotheses · 5 findings · 38 tasks · 33m 56s
   [View Findings] [Export Report]
   ```

### 3.3 查看 Finding

- 在聊天流中点击 Finding 卡片 → 右侧滑出 **详情抽屉**。
- 抽屉内容：
  - 标题 + severity 徽章
  - 描述（Markdown）
  - Critic Note（引用块）
  - Evidence 代码块
  - 关联 Hypothesis 链接
  - [Copy] [Export this Finding] 按钮

### 3.4 暂停/停止

- 进度卡片右上角显示 Pause / Stop 按钮。
- 暂停后：
  - 输入框占位符变为 "Paused. Type /resume to continue."
  - 进度卡片显示 "⏸ Paused"
- 停止后：
  - 保存 Blackboard。
  - 显示 "Analysis stopped. Workspace saved."
  - 提供 `/resume <workspace>` 命令。

### 3.5 恢复历史分析

用户操作：

```text
/resume fastjson2.git_20260408_225653
```

或左侧 Workspaces 列表点击。

系统响应：

1. 从 `.blackboard.json` 加载状态。
2. 在聊天流顶部显示 "Resumed workspace `fastjson2...`"
3. 把已有的 Findings 和 Hypotheses 以卡片形式重新渲染到聊天流中（或只渲染摘要卡片 + "查看详情" 按钮）。
4. 继续 Cerebrum 循环。

### 3.6 导出报告

命令：

```text
/export
/export --format json
```

或点击 Finding/摘要卡片上的 Export 按钮。

输出：

- Markdown 报告：人类可读，含表格、代码块。
- JSON 报告：机器可读，含全部 hypotheses / findings。
- DOT/GraphML：可选，用于图可视化。

保存路径显示在聊天流中，可点击打开目录。

---

## 4. 聊天消息卡片规范

### 4.1 用户消息

```
┌──────────────────────────────────────────┐
│ scan ./myproject --workers 8             │
│                              10:42       │
└──────────────────────────────────────────┘
                         右对齐，紫色背景
```

### 4.2 系统状态消息

```
─── Analysis started on ./myproject ───
```

居中、灰色、小字。

### 4.3 进度卡片

```
┌──────────────────────────────────────────┐
│ 🧠 Round 3 / 30                          │
│ ⏱  00:02:14    🚁 5 drones    🐞 2 found │
│ ████████░░░░░░░░░░░░ 35%                 │
│ [Pause] [Stop]                           │
└──────────────────────────────────────────┘
```

实时更新同一卡片（用 id 标识），不刷屏。

### 4.4 Hypothesis 卡片

```
┌──────────────────────────────────────────┐
│ 🧬 H-9BDEBF  [active]                    │
│ AutoType blacklist in ObjectReaderProvider│
│ can be bypassed...                       │
│ conf: ████████████░░ 100%                │
│ 7 tasks · 4 evidence                     │
└──────────────────────────────────────────┘
```

状态颜色：
- pending：灰色
- active：蓝色
- suspected：黄色
- confirmed：绿色
- discarded：暗色删除线

### 4.5 Finding 卡片

```
┌──────────────────────────────────────────┐
│ │ 🟠 HIGH                                │
│ │ F-C224AD                               │
│ │ ObjectReaderProvider.checkAutoType...  │
│ │ linked to H-9BDEBF                     │
│ │ [View Details] [Export]                │
└──────────────────────────────────────────┘
```

左侧色条粗细 4px，颜色对应 severity。

### 4.6 代码证据卡片

```
┌──────────────────────────────────────────┐
│ Evidence: ObjectReaderProvider.java      │
│ ┌────────────────────────────────────┐   │
│ │ static final String[] DENYS;       │   │
│ │ ...                                │   │
│ └────────────────────────────────────┘   │
│ [Copy]                                   │
└──────────────────────────────────────────┘
```

### 4.7 摘要/结束卡片

```
┌──────────────────────────────────────────┐
│ ✅ Analysis Complete                     │
│ 56 hypotheses · 5 findings · 38 tasks    │
│ Duration: 33m 56s                        │
│ [View Findings] [Export MD] [Export JSON]│
└──────────────────────────────────────────┘
```

---

## 5. CLI 交互

### 5.1 启动扫描

```bash
zdll scan https://github.com/alibaba/fastjson2.git --workers 8
```

输出示例：

```text
🧠 zdll Hive-Mind
Target: https://github.com/alibaba/fastjson2.git
────────────────────────────────────────
Round 1/30  [==>               ]  8%  drones:5  findings:0
🧬 H-9BDEBF  [active]  conf: 85%  AutoType blacklist...
🚁 Drone T-001 launched [evidence-collector]
✅ Drone T-001 completed
🧠 Round 2/30  [====>             ] 16%
🟠 FINDING [HIGH] F-C224AD: ObjectReaderProvider.checkAutoType...
...
✅ Analysis complete
56 hypotheses · 5 findings · 38 tasks · 33m 56s
Report: /home/user/zdll_workspace/fastjson2.git_20260408_225653/reports/report.md
```

### 5.2 查看报告

```bash
zdll report fastjson2.git_20260408_225653
```

默认打开 Markdown 报告路径。

---

## 6. 错误与降级

| 场景 | 交互 |
|---|---|
| API Key 缺失 | 输入框上方显示红色提示，自动打开 Settings |
| Kimi CLI 未安装/找不到 | 系统消息提示下载链接，[Auto Download] 按钮 |
| osv-scanner 缺失 | 自动从 Release 下载；失败则跳过依赖扫描 |
| tree-sitter 某语言缺失 | 降级为 regex，提示 "semantic analysis limited" |
| 目标路径不存在 | 用户消息变红，提示重新输入 |
| LLM 返回非 JSON | Cerebrum 内部重试，前端显示 "retrying round N" |

---

## 7. 与解耦架构的对应

```
Frontend ──Wails Binding──> internal/app (Scan/Resume/Stop/Export)
                              │
                              ▼
                        internal/core (Engine + Blackboard)
                              │
                              ▼
                        eventbus.Bus
                              │
        ┌─────────────────────┴─────────────────────┐
        ▼                                             ▼
   CLI 彩色日志                                   Vue 聊天卡片
```

- **操作可落地**：每个交互都对应 `internal/app` 的一个方法。
- **交互可替换**：CLI 和桌面是同一 Bus 的不同消费者，未来加 Web UI 只需再写一个消费者。
- **结果展示可扩展**：新增报告格式只需实现 `report.Renderer`，前端无需改动。

## 8. 核心 vs 非核心

### 核心（MVP 必须）

- 启动/恢复分析
- 实时事件流（Round / Hypothesis / Drone / Finding）
- Finding 卡片 + 详情抽屉
- 摘要卡片
- 暂停/停止/恢复
- Markdown/JSON 报告导出
- 历史工作区列表
- 基础 Settings

### 非核心（后续增强）

- Exploit Chain 图可视化
- Hypotheses 树形图
- 多模型对比
- 团队协作
- PoC/Fuzzer（已去掉）
- 审批弹窗（YOLO 已默认开启）
