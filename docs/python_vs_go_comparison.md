# zerodayFind Python vs Go（zdll）实现对比报告

> 生成时间：2026-06-26  
> 范围：Python 实现（仓库根目录 `.py` 与 `engine/`） vs Go 实现（`zdll/`）  
> 目的：梳理两套实现的能力差异、架构差异与实现细节，为后续功能补齐与质量对齐提供依据。

---

## 1. 总体概览

| 维度 | Python 实现 | Go 实现（zdll） |
|------|-------------|-----------------|
| **语言 / 运行时** | Python 3.11+，asyncio | Go 1.25，goroutine + context |
| **CLI 入口** | `kimi.py`、`kimi_hive.py` | `zdll/cmd/zdll/main.go`（Cobra） |
| **核心子命令** | `scan`、`serve`、`worker`、`feed`、`binrev` | `scan`、`resume`、`ci`、`report`、`list`、`config` |
| **架构角色** | `Cerebrum` + `Drone` + `Blackboard` | `Engine` + `DronePool` + `BlackboardManager` |
| **LLM 运行时** | `kimi_agent_sdk.Session`（Kimi CLI runtime） | 自研 Eino runner：`CerebrumRunner`、`DroneGraphRunner`、`DroneRunner` |
| **工具来源** | Kimi CLI 内置工具：`ReadFile`、`GrepLocal`、`Shell`、`Glob` 等 | 原生 Eino tool：`bash`、`read_file`、`write_file`、`grep_search`、`glob`、`fetch_url` |
| **并发模型** | asyncio + 全局 `DroneSessionPool` 预热 | goroutine + 有界 `DronePool` |
| **持久化** | `LocalBackend` JSON / `RedisBackend` / `IncrementalLocalBackend` | `JSONStore` 原子写 `.blackboard.json` |
| **沙箱** | `root_dir/tmp/.drone_sandboxes/<task_id>` + `_target` symlink | 同路径 + `_target` symlink，Windows 回退到 directory junction |
| **测试覆盖** | 仅 `tests/test_sector.py` | `internal/core`、`internal/agent`、`internal/exploit`、`internal/report` 等均有测试 |

**一句话总结**：Python 版依赖成熟的 Kimi CLI Agent SDK 与内置工具，重“ prompt + 会话管理”；Go 版自研了工具调用循环与 Eino 运行时，重“代码级可控 + 结构化状态机”。

---

## 2. 编排循环对比

### 2.1 Python：`engine/cerebrum.py`

- 入口：`Cerebrum.launch()`（`cerebrum.py:559-733`）。
- 流程：
  1. 建立 LLM session。
  2. 超大项目切分为 `Sector`（`sector.py`）。
  3. 注入 OSV 依赖漏洞发现（`_inject_osv_findings`）。
  4. Round 0 文档智能（`_run_doc_intelligence`）。
  5. 检测 domain，加载 terrain，构建 phase pipeline。
  6. 启动 `_drone_dispatcher` 与 `_result_integrator`，进入 `_cerebrum_loop`。
- 解析：`_parse_output` → JSON 优先，XML legacy fallback。
- 终止：`TerminationGuard` 5 层（预算、收敛、停滞、LLM 自声明、自然语言软终止）。
- 默认预算：`max_rounds=30`、`max_tasks=200`、`max_time=7200s`、`stagnation=3/5`。

### 2.2 Go：`internal/core/engine.go`

- 入口：`Engine.Run()` → 大项目走 sector coordinator，否则 `runSingle()`。
- 流程：
  1. `DocIntel.Gather()` 收集文件树、语言、依赖清单、入口点。
  2. 运行 scanners（OSV + semantic）。
  3. 每轮：`buildSynthesisPrompt` → `runAgent` → `parseAndApply` → 派发 drones → 等待 → `integrateResults`。
  4. 偶数轮调用 `critic.Review()`。
  5. 触发 `scheduleDevilsAdvocate`、`autoPromoteHighConfidenceHypotheses`、`sweepOrphanedHypotheses`、Bayesian 更新。
- 解析：`extractJSON`（支持字符串内大括号、未闭合 fence fallback） + XML fallback + simple finding regex。
- 终止：`TerminationGuard.Check()`（`internal/core/guard.go`）+ 软终止。
- 默认预算：与 Python 基本一致。

### 2.3 关键差异

| 能力 | Python | Go |
|------|--------|-----|
| 大项目拆分 | `SectorManager` + `BlackboardPartition` | `SectorCoordinator` + `MergeFrom` |
| Session 预热 | 全局 `DroneSessionPool` | 无（每任务新建 runner/session） |
| 异步结果整合 | `_result_integrator` 后台任务 | 每轮等待当前 batch 后同步整合 |
| Drone 重试 | 外层 `_DRONE_TIMEOUT` + `_MAX_RETRIES=2` | `DronePool.Submit` 内部重试 2 次 |
| 解析容错 | JSON/XML fallback | JSON（字符串感知）+ XML + simple finding regex |

---

## 3. Drone 执行与沙箱

### 3.1 Python：`engine/drone.py`

- 沙盒：`root_dir/tmp/.drone_sandboxes/<task_id>`。
- `_ensure_target_link()`：创建 `_target` symlink 指向原项目；Windows 无权限时回退到直接使用 `work_dir`。
- Session：`kimi_agent_sdk.Session.create(work_dir=KaosPath(sandbox))`，使用 SDK 默认工具集。
- 追逐轮次：`MAX_CHASE_ROUNDS = 3`（安全角色），透传角色 1 轮。
- Prompt：包含 `REACHABLE`/`EXPLOITABLE`/`MITIGATED` 筛查问题与固定输出格式。
- 解析：`_parse_round_output` 提取 `FINDING`、`SEVERITY`、`CONFIDENCE`、`EVIDENCE`、`DETAIL`、`TRACE_TARGET`。
- 工具：由 Kimi CLI runtime 提供，Python 代码未自定义 read/grep 逻辑。

### 3.2 Go：`internal/core/drone.go` + `internal/agent/*`

- 沙盒：同路径；Windows 用 `mklink /J` directory junction，保证无需管理员权限。
- Runner：
  - `CerebrumRunner`：无工具单轮生成。
  - `DroneGraphRunner`：基于 Eino `compose.Graph` 的工具调用图（默认）。
  - `DroneRunner`：手写的 ReAct 循环（兼容兜底）。
- 工具：自定义 `ToolSet`（`internal/agent/tools.go`）。
- 追逐：`Drone.runChase()` 多轮，预算 1200s，根据 `TRACE_NEEDED` 与置信度决定是否继续。
- Prompt：`buildDroneSystemPrompt` 动态检测 `_target/` 并强制前缀；否则使用相对路径。
- 解析：`parseRoundOutput` 提取 screening 字段与 finding。

### 3.3 工具能力细节对比

| 工具 | Python（Kimi CLI） | Go（zdll 自研） |
|------|--------------------|-----------------|
| **read_file** | `ReadFile`：按行读取，最大 1000 行 / 100KB，返回行号 | `read_file`：按字节读取，默认 256KB，最大 1MB；已增加大小写模糊匹配 |
| **grep/search** | `GrepLocal` | `grep_search`：正则，最多 50 条结果 |
| **shell** | `Shell` / `Terminal`（ACP） | `bash`：固定 blocklist，默认 60s |
| **glob** | 有 | 有 |
| **write_file** | 有 | 有 |
| **fetch_url** | 未在工具列表明确看到 | 有 |
| **错误返回** | `ToolError` 结构化对象 | 字符串 `[error] ...`，空结果替换为 `[no output]` |
| **路径容错** | 无模糊匹配 | 精确路径缺失时做大小写不敏感 basename 搜索 |

---

## 4. Blackboard 状态管理

### 4.1 Python：`engine/blackboard.py`

- 核心类型：`HypothesisNode`、`DroneTask`、`Finding`、`ExploitPrerequisites`。
- Hypothesis 状态：`PENDING`、`ACTIVE`、`SUSPECTED`、`CONFIRMED`、`DISCARDED`。
- 去重：
  - `BloomDeduplicator` 预过滤。
  - `_find_similar_hypothesis()` 4 层语义匹配：anchor → structural fingerprint → bigram Jaccard → unigram Jaccard → char trigrams。
- 提升：
  - `_auto_promote_high_confidence_hypotheses`：`confidence >= 0.80`（含代码引用）或 `>= 0.90`。
  - `_sweep_orphaned_hypotheses`：`CONFIRMED`/`SUSPECTED` 或 `confidence >= 0.75`。
- 分区：`BlackboardPartition` 为每个 Sector 隔离状态，findings 自动冒泡到父 Blackboard。
- 持久化：local JSON / Redis / incremental；原子写。

### 4.2 Go：`internal/core/blackboard.go` + `types.go`

- 核心类型：`HypothesisNode`、`DroneTask`、`Finding`。
- Hypothesis 状态：`pending`、`active`、`suspected`、`confirmed`、`discarded`。
- 去重：
  - `findSimilarHypothesis()` 5 层：anchor match → structural fingerprint → bigram Jaccard → unigram Jaccard → char-trigram fallback。
  - Task 按 hypothesis+role 去重；Finding 按 title token Jaccard ≥ 0.6 去重。
- 提升（近期修复后）：
  - `autoPromoteHighConfidenceHypotheses`：要求 `[confirmed]`/`[critic-accepted]` 证据，且负面证据比例 < 33%。
  - `sweepOrphanedHypotheses`：Layer 1 为 confirmed/suspected ≥ 0.5；Layer 2 要求 ≥ 0.75 且含正面证据，负面比例 < 40%。
- 拒绝：`RejectFinding()` 设置 `Status=rejected`、`Severity=none`、记录原因；`ActiveFindings()` 自动过滤 rejected。
- 持久化：`JSONStore` 原子写；resume 仅当 target 为空时加载现有 blackboard。

### 4.3 关键差异

- Python 有 Sector partition 的完整隔离模型；Go 通过 `MergeFrom` 合并各 sector 结果。
- Go 明确支持 finding 状态 `rejected` 与 `needs_review`，并在所有报告渲染器中排除 rejected；Python 的 Critic `REJECT` 不生成 Finding，因此不存在“已生成后撤回”的状态。
- Go 的提升逻辑更严格（需正面证据、负面比例限制）。

---

## 5. Critic / 对抗评审

### 5.1 Python：`engine/cerebrum.py:_run_critic()`

- 实现位置：内联在 `Cerebrum` 中，非独立模块。
- 决策：`ACCEPT` / `REJECT`（解析正则只识别这两种）。
- 失败策略：fail-open，全部默认 `ACCEPT`（超时、step limit、异常、解析失败）。
- 调用：每个 positive drone 输出后调用。
- 结果：
  - `ACCEPT` → 生成/更新 Finding，Critic 理由写入 hypothesis evidence `[critic-accepted]`。
  - `REJECT` → 发出 `finding_rejected_by_critic` 事件，不生成 Finding。

### 5.2 Go：`internal/core/critic.go`

- 实现位置：独立 `Critic` 结构体，通过 `Engine.WithCritic()` 注入。
- 两个入口：
  - `Critic.Review()`：每偶数轮批量评审所有假设/发现。
  - `Critic.Judge()`：每个 positive drone 输出后逐条评审。
- 决策：`ACCEPT` / `REJECT` / `NEEDS_REVIEW`。
- 失败策略：fail-open，默认 `ACCEPT`（runner 缺失、超时、错误、XML 不可解析）。
- 结果：
  - `ACCEPT` → 生成 Finding，可附带 severity 调整。
  - `REJECT` → `applyNegativeResult` 扣分， confidence 崩溃则丢弃假设，负面证据占优则拒绝已有 finding。
  - `NEEDS_REVIEW` → 不生成 Finding，但**不扣分**，保留假设等待更多证据。

### 5.3 关键差异

| 维度 | Python | Go |
|------|--------|-----|
| 模块独立性 | 内联在 Cerebrum | 独立 `Critic` 模块 |
| 决策集合 | `ACCEPT` / `REJECT` | `ACCEPT` / `REJECT` / `NEEDS_REVIEW` |
| 失败策略 | fail-open ACCEPT | fail-open ACCEPT |
| NEEDS_REVIEW 语义 | 无（解析不到默认 ACCEPT） | 明确：保留假设、不扣分 |
| Rejected finding 状态 | 不生成 Finding | 生成后可 `RejectFinding` 并排除 |

---

## 6. Exploit 前置条件分析

### 6.1 Python：`tools/exploit_analyzer.py`

- 双层分析：
  1. **静态规则层**：关键词/正则推断 auth、network、privilege、env configs、tools、CVEs。
  2. **LLM 推断层**：调用项目内 LLM 客户端做语义推断；仅当 `confidence >= 0.6` 时覆盖静态结果。
- `is_satisfiable`：基于 `target_env` 判断；无环境信息且存在 `env_configs` 时保守标记为 `False`。
- 默认网络：external。

### 6.2 Go：`internal/exploit/analyzer.go`

- 仅**静态规则层**（与 Python 静态规则对齐）。
- 无 LLM 推断层。
- `is_satisfiable`：通过**矛盾检测**计算：
  - `auth_required` 但描述含 public/unauthenticated → `False`。
  - 要求 admin/system 权限但描述为 public/unauthenticated → `False`。
- `analyzeAndAdjudicateFinding` 会对不可满足的 zero-day finding 直接 `RejectFinding`。

### 6.3 关键差异

| 维度 | Python | Go |
|------|--------|-----|
| 分析方式 | 静态 + 可选 LLM | 仅静态 |
| `is_satisfiable` 依据 | `target_env` 对比 | 内部矛盾检测 + `target_env`（未传入时依赖矛盾） |
| 不满足处理 | 标记 `is_satisfiable=False` | 直接 `RejectFinding`（zero-day） |
| 主要差距 | Go 缺少 LLM 语义推断层 | — |

---

## 7. 领域（Domain）检测与 Skills 加载

### 7.1 领域检测

- **两者均为关键词/正则匹配**，无真正的语义/LLM 分类器。
- **Python**：`engine/cerebrum.py:_detect_domains_from_intel()`，13 个领域（含强制的 `security-expert`）。
- **Go**：`internal/core/domain.go`，12 个领域 + `foundation-lib` 兜底 + 强制的 `security-expert`。
- 9 个 phase 顺序相同：`target-definer` → `code-understander` → `vuln-hunter` → `hypothesis-tester` → `variant-analyzer` → `validator` → `exploit-builder` → `poc-generator` → `report-generator`。

### 7.2 `security-expert/SKILL.md` 预加载

| 维度 | Python | Go |
|------|--------|-----|
| 加载时机 | `_setup_session()` 阶段，在 Round 0 之前 | `NewEngine()` 阶段读取并缓存 |
| 注入位置 | Cerebrum system prompt | `buildSynthesisPrompt()` 中 system prompt 之后 |
| 用途 | 作为通用安全路由表，指导 Round 0 的领域识别 | 与 Python 对齐，提供通用安全路由表 |

### 7.3 Terrain / Phase Pipeline

- 两者均在 doc-intel 完成后检测 domain，然后：
  1. 读取 `skills/<domain>/SKILL.md` 生成 terrain（均跳过 `security-expert`，因为它已在 system prompt 中预加载）。
  2. 遍历 `PhaseOrder`，合并所有含 `references/` 目录的领域的 `references/<phase>.md`。
  3. 为每个 phase 设置 `MaxRounds`。
- **实现位置**：Python `engine/cerebrum.py:_load_domain_terrain()` / `_build_domain_pipeline()`；Go `internal/core/domain.go:loadTerrain()` / `buildPipeline()`。

### 7.4 Phase 推进

| 维度 | Python | Go |
|------|--------|-----|
| 推进条件 | `phase_complete=true` **或** `is_complete=true` **或** 阶段轮次耗尽 | 阶段轮次耗尽 **或** `phase_complete=true`（已补齐） |
| LLM 自声明字段 | `phase_complete`、`is_complete` | `phase_complete`、`is_complete` |
| 阶段切换等待 | `_wait_for_drain(min_wait=3.0)` | 当前轮次结束、batch 完成后自然切换 |

### 7.5 Prompt 工程对齐

Go 的 `buildSynthesisPrompt()` 已补齐 Python `_build_synthesis_prompt()` 中的关键元素：

| Prompt 元素 | Python | Go |
|-------------|--------|-----|
| `security-expert` 路由表 | ✅ `_setup_session()` | ✅ `securityExpertBrief` |
| Domain terrain | ✅ `_load_domain_terrain()` | ✅ `loadTerrain()` |
| Phase methodology | ✅ `_build_phase_prompt()` / `_build_synthesis_prompt()` | ✅ `CurrentPhaseSection()`（含 round 进度） |
| `phase_complete` 字段要求 | ✅ | ✅ `prompt.go` + `parseAndApply` |
| 独立 OSV 依赖漏洞区块 | ✅ `## Dependency Vulnerability Audit` | ✅ `dependencyFindingSummary()` |
| 已派发任务去重列表 | ✅ `Already Dispatched Tasks` | ✅ `dispatchedTasksSummary()` |
| Cognitive Triggers | ✅ ASSUMPTION AUDIT / ANOMALY REFLECTION / SELF-FALSIFICATION | ✅ 同文本注入 |
| Weaponization 反惰性指令 | ✅ exploit-builder/poc-generator 强制 PoC | ✅ 同条件注入 |

> 结论：Skills 体系（检测、加载、注入、阶段推进、Prompt 工程）已与 Python 实现对齐。

---

## 8. 扫描器与初始信号

### 8.1 依赖漏洞扫描

- **Python**：`engine/osv_bridge.py`，自动查找 `bin/osv-scanner` / `OSV_SCANNER_PATH` / PATH，运行 `osv-scanner scan source -r --format json`，注入为 `finding_type=dependency_vuln`。
- **Go**：`internal/scanner/osv.go`，仅在找到二进制时才运行（`OSV_SCANNER_PATH`、PATH、`cfg.Paths.OSVScanner`、`<repo>/bin/osv-scanner`），生成 `FindingTypeDependency`。

差异：Python 更积极查找 osv-scanner；Go 无自动下载。

### 8.2 语义信号 / Tree-sitter

- **Python**：`engine/semantic_helper.py` 提供 AST 函数列表、引用查找、调用图等；被 engine 调用生成信号，具体是否作为 finding 需看调用点。
- **Go**：`internal/scanner/semantic.go` / `internal/core/tree_sitter.go`，扫描 sink 调用（`exec`、`eval`、`Query` 等），但语义信号**不进入 blackboard 作为 finding**，仅用于 seed Cerebrum hypotheses。

差异：Python 的语义信号更可能被当作初始证据使用；Go 明确把语义信号当作 hypothesis 种子。

---

## 9. 报告输出

| 格式 | Python | Go |
|------|--------|-----|
| Markdown | ✅ | ✅ |
| JSON | ✅ | ✅ |
| SARIF | ❌ | ✅ |
| DOT / GraphML | ✅ | ✅ |
| 基线对比（baseline） | ❌ | ✅ |
| CI 模式 | ❌ 明确 | ✅ `zdll ci` |
| rejected 排除 | 不生成即排除 | `ActiveFindings()` 过滤 |

---

## 10. 构建、测试与工程化

| 维度 | Python | Go |
|------|--------|-----|
| 依赖安装 | `pip install -r requirements.txt` | `go mod download` |
| 测试命令 | `python -m pytest` | `go test ./...` |
| 代码风格 | `black`/`ruff`/`mypy`（配置在 `pyproject.toml`） | `gofmt` + `go vet`（无额外 lint 配置） |
| 测试文件 | 仅 `tests/test_sector.py` | `internal/*/...` 多处测试 |
| CGO | 依赖 tree-sitter（有 C 扩展） | 依赖 `go-tree-sitter`（需 gcc/MinGW） |
| Windows 构建 | 通常无额外要求 | 需 MinGW 或 Zig wrapper 作为 `CC` |
| 配置问题 | `pyproject.toml` 中包名 `clawdboz` 与仓库结构不符，疑似陈旧配置 | `go.mod` `module zdll`，与目录结构一致 |

---

## 11. 能力差距矩阵

| 能力 | Python | Go | 备注 / 建议 |
|------|--------|-----|-------------|
| 基础假设驱动扫描 | ✅ | ✅ | 核心流程一致 |
| Drone 沙箱 + `_target` | ✅ symlink | ✅ symlink/junction | Windows 下 Go 的 junction 更稳定 |
| 工具调用可控性 | 中（SDK 封装） | 高（自研） | Go 更易调优工具行为 |
| Session 预热池 | ✅ DroneSessionPool | ✅ per-role DronePool (`internal/agent/pool.go`) | 均已实现 |
| Sector 大项目拆分 | ✅ 完整 | ✅ 基本 | Python 的 partition 模型更成熟 |
| Critic 独立模块 | ❌ 内联 | ✅ 独立 | Python 可抽离模块；Go 语义更完整 |
| Critic NEEDS_REVIEW | ❌ | ✅ | Python 只有 ACCEPT/REJECT |
| Exploit Analyzer LLM 层 | ✅ 静态 + 可选 LLM | ✅ 静态 + 可选 LLM（`internal/exploit`） | 已对齐 |
| Exploit 矛盾检测 | ⚠️ 依赖 target_env | ✅ 内置 | Go 更主动拒绝不可利用发现 |
| 依赖扫描（OSV） | ✅ 主动找二进制 | ✅ env → config → `<target>/bin/osv-scanner` → PATH | 已对齐 |
| 语义信号作为 finding | 可能 | ❌ 仅 seed | 设计选择差异 |
| 报告 SARIF / CI | ❌ | ✅ | Python 可借鉴 |
| 大小写路径容错 | ❌ | ✅ | Python 可同步 |
| 测试覆盖 | ⚠️ 极少 | ✅ 较多 | Python 需补测试 |
| 文档/提示路径指引 | 宽松 | 强制 `_target/` | 两者各有优劣 |
| Skills 体系（domain 检测、terrain、phase pipeline、Prompt 工程） | ✅ | ✅ | 已对齐 |
| Finding 质量控制（负结果过滤 / 空证据 reject / 置信度门槛 / 去重） | ⚠️ 部分在 prompt/策略中 | ✅ `internal/core` + `internal/report` 显式过滤 | Go 更严格 |

---

## 12. 近期已修复的 Go 端问题

以下问题已在本次迭代中修复并验证（`go test ./...` 通过）：

1. **Critic panic**：XML fallback 正则使用了 Go 不支持的 `\1` 回溯引用 → 改为手动匹配开闭标签。
2. **JSON 解析截断**：`extractJSON` 对大括号在 JSON 字符串内的对象会提前截断 → 改为字符串感知的 `balanceBraces`。
3. **read_file 幻觉路径**：精确路径缺失时直接报错 → 增加大小写不敏感 basename 搜索，并给出同目录文件提示。
4. **Critic 失败级联**：超时/错误默认 `NEEDS_REVIEW` 导致大量假阳性 → 改为 fail-open `ACCEPT`。
5. **sweep 过度收割**：高置信度裸假设被提升为 finding → 增加正面证据与负面比例限制。
6. **ExploitPrerequisites 占位**：`IsSatisfiable` 原为硬编码 `true` → 改为静态规则 + 矛盾检测。
7. **Finding 撤回**：新增 `RejectFinding`、rejected 状态、事件与报告排除。
8. **Skills 体系对齐**：
   - 预加载 `security-expert/SKILL.md` 作为 Cerebrum 路由表。
   - 支持 LLM 通过 `phase_complete` 主动推进阶段。
   - Prompt 中增加已派发任务列表、Cognitive Triggers、weaponization 反惰性指令、独立 OSV 依赖漏洞区块。

---

## 13. 建议

### 短期（保持 Go 版本可用）

1. **补齐 Exploit Analyzer 的 LLM 层**：在 `internal/exploit` 中增加可选 LLM 推断接口，默认静态，配置开启时调用 LLM，与 Python 行为对齐。
2. **路径指引 A/B 测试**：当前强制 `_target/` 前缀；可对比 Python 的“如果存在就用”提示，观察是否减少模型幻觉。
3. **持续增加回归测试**：尤其是 `parseAndApply`、Critic 决策、路径模糊匹配等关键路径。

### 中期（能力对齐）

4. **Sector partition 模型**：Go 当前以 `MergeFrom` 合并结果，可参考 Python `BlackboardPartition` 实现更严格的 per-sector 状态隔离与 findings 冒泡。
5. **统一报告能力**：Python 端可引入 SARIF 输出与 CI 模式；Go 端可引入更多统计摘要。

### 长期（架构选择）

6. **决定是否保留“自研 Eino runner”**：Go 的优势是可控；劣势是需持续追平 Kimi CLI 工具能力。若 Kimi CLI 工具持续演进，需评估维护成本。
7. **Python 端补测试与清理配置**：至少为 `Blackboard` 去重、`Critic`、`ExploitAnalyzer`、`OSVBridge` 增加单元测试；修正 `pyproject.toml` 中的包名/入口点。

---

## 14. 结论

- **Python 实现更“重 SDK、轻自研”**：利用 Kimi CLI Agent SDK 的 session、工具与运行时，代码集中在 prompt 工程、状态机和编排策略上。
- **Go 实现更“重自研、轻依赖”**：自研了 Eino runner、工具集、Blackboard 状态机，提供了更强的可观测性与可控性，但也意味着需要自行补齐 Kimi CLI 已具备的一些能力（如 session 池、LLM exploit 推断）。
- **核心安全分析流程已基本对齐**：假设-任务-drone-证据-评审-报告的闭环在两边都已跑通。
- **Skills 体系已对齐**：domain 检测、terrain 加载、phase pipeline、Prompt 工程（`security-expert` 路由表、`phase_complete`、Cognitive Triggers、weaponization 指令、OSV 区块）已实现一致。
- **主要差距集中在工程成熟度**：Python 的 sector partition、LLM exploit 推断更成熟；Go 的报告格式（SARIF/CI）、路径容错、测试覆盖更领先。

建议后续以 **“Go 作为主力执行引擎 + Python 作为能力参考”** 的方式继续迭代，把 Python 中经过验证的能力（session 池、LLM exploit 推断、sector partition）逐步迁移到 Go，同时保持 Go 在可控性与测试覆盖上的优势。
