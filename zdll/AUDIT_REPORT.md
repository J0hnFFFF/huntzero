# zdll 代码审计报告

> **审计日期**: 2026-06-25
> **项目版本**: 0.1.0 (HIVE-MIND INTEL ENGINE V8.0)
> **审计范围**: 全量 52 个 Go 源文件 (32 生产 + 20 测试)
> **审计维度**: 安全性、正确性、架构设计、可维护性、性能

---

## 一、项目概要

**zdll** (Zero-Day Local LLM Hunter) 是 kimiSec Hive-Mind 安全分析引擎的 Go 重制版 — 一个使用 Kimi 大模型驱动的零日漏洞自动发现工具。架构采用 **Cerebrum → Hypothesis → Drone → Finding** 的四阶段流水线：

```
DocIntel → Scanner → Engine(Cerebrum) → DronePool → Critic → Report
                         ↕
                    BlackboardManager (状态中心 + 持久化)
```

| 模块 | 角色 | 核心组件 |
|------|------|----------|
| `cmd/zdll` | CLI入口 | Cobra命令树, 事件驱动CLI日志 |
| `internal/app` | 应用服务层 | 目标解析、报告导出、变更路径过滤 |
| `internal/core` | 核心引擎 | Engine, Blackboard, Drone, SessionPool, Sector, Critic |
| `internal/llm` | LLM接入 | Kimi SDK封装, Agent YAML生成 |
| `internal/scanner` | 扫描器 | Semantic(tree-sitter), OSV(依赖) |
| `internal/report` | 报告 | Markdown/JSON/SARIF/DOT/GraphML |

---

## 二、总体评价

**代码质量: 中上。** 项目架构清晰，模块划分合理，状态机设计完整。去重管线（Bloom + 五层语义）和分区编排（Sector + Coordinator）是亮点。但若干安全问题需要在生产使用前修复，部分架构设计存在并发安全隐患。

**优秀的点** 🟢:
- 五层语义去重管线（anchor → structural → bigram → unigram → char trigram）设计精巧
- BlackboardManager 使用读写锁保证线程安全，API 设计一致
- JSONStore 使用原子写入（tmp + rename）避免损坏
- Drone 多轮追踪机制（budget/timeout）、chase决策、证据链合并
- Sector 分区编排支持大型项目自动分解

**需要关注的点** ⚠️:
- 安全：二进制自动下载无校验、API Key可能被日志泄露
- 并发：Snapshot() 返回裸指针、Coordinator 缺少 context 传播
- 质量：错误处理大量 `_, _ =` 忽略、重复代码、缺少关键模块测试

---

## 三、🔴 Blocker 级问题（必须修复）

### 🔴 1. 供应链安全：OSV-Scanner 自动下载未做完整性校验

**文件**: `internal/scanner/osv.go:157-207` (`downloadOSVScanner()`)
**严重度**: High

```go
url := fmt.Sprintf("https://github.com/google/osv-scanner/releases/download/v%s/%s", osvScannerVersion, assetName)
// ...
resp, err := client.Get(url)  // ❌ 无SHA256校验, 无GPG验签
```

**风险**: 如果 GitHub 被攻击或中间人拦截，下载的二进制可能是恶意代码。`osv-scanner` 随后被直接执行 (`exec.CommandContext`)，这意味着任意代码执行。

**建议**: 
- 添加已知的 SHA256 校验和（hardcode 在代码中），下载后校验
- 考虑使用 `go-sumdb` 风格的校验机制
- 至少对 HTTP 响应使用 TLS 证书校验（目前 http.Client 默认已启用）

### 🔴 2. API Key 泄露风险

**文件**: `cmd/zdll/main.go:480-483` (`runConfigValidate()`)
**严重度**: Medium

```go
masked := cfg.LLM.APIKey
if len(masked) > 8 {
    masked = masked[:4] + "..." + masked[len(masked)-4:]
}
fmt.Fprintf(out, "API Key:  %s (set)\n", masked)
```

**风险**: 
1. 若 API Key 恰好 ≤ 8 字符，**完整打印**（无掩码）
2. 即使遮盖，也泄露了前缀4字符和后缀4字符 — 这在某些泄露场景下可大幅缩小暴力搜索空间
3. 若 `--quiet` 未设置，CLI 输出到 stdout 可能被 CI 日志系统持久化

**建议**: 始终打印固定掩码，如 `sk-****`，不暴露任何实际字符。

### 🔴 3. 并发安全：Snapshot() 返回裸指针导致数据竞争

**文件**: `internal/core/blackboard.go:261-265`
**严重度**: High

```go
func (bm *BlackboardManager) Snapshot() *Blackboard {
    bm.mu.RLock()
    defer bm.mu.RUnlock()
    return bm.bb  // ❌ 返回内部指针, 锁释放后调用者可并发修改
}
```

**风险**: 
- `Snapshot()` 获取读锁后返回 `*Blackboard` 指针。锁释放后，其他 goroutine 可同时通过 `AddFinding()` 等持有写锁修改该结构，导致 **数据竞争**。
- 典型的 race condition: `integrateResults()` 遍历 `bm.Snapshot().Tasks` 的同时，`AddTask()` 在修改 `bm.bb.Tasks`。
- 当前多处在循环中使用此模式，如 `engine.go:320 for _, h := range bm.Snapshot().Hypotheses`

**建议**: 
- 选项A（推荐）：返回深拷贝（性能开销取决于 Blackboard 大小）
- 选项B：`Snapshot()` 不释放锁直到调用者完成（需要调用者显式 `Release()`）
- 选项C：所有只读操作通过 `Snapshot()` 间接访问，在调用处持锁

### 🔴 4. Coordinator context 未传播到子 goroutine

**文件**: `internal/core/sector.go:325-348` (`Coordinator.Run()`)
**严重度**: Medium

```go
func (c *Coordinator) Run(ctx context.Context, ...) error {
    // ...
    for i := range sectors {
        go func(sector *Sector) {
            c.runSector(ctx, parent, sector, ...)  // ❌ 使用外部ctx但未检查取消
        }(&sectors[i])
    }
}
```

**风险**: 在 `runSector()` 中创建了新的 Engine 并调用 `engine.Run()`，但 Coordinator 的 `ctx` 取消后，这些子 goroutine 不会收到通知（除非 Engine 内部有 `ctx.Done()` 检查，但实际上 Engine.Run 内部只在 `runSingle` 的主循环中检查了一次）。子 Engine 可能长时间运行，浪费 API 调用配额。

**建议**: 在 `runSector()` 中使用 `context.WithCancel(ctx)` 派生子 context，并在 `defer cancel()` 中确保取消传播。

### 🔴 5. LLM输出直接写入文件系统的安全风险

**文件**: `internal/core/drone.go:519-570` (`extractPocsFromText()`)
**严重度**: Medium

```go
_ = os.WriteFile(targetPath, []byte(trimmed+"\n"), 0o644)
```

**风险**: Drone 从 LLM 输出中提取代码块并写入本地文件系统。虽然 prompt 中包含了 `[HARD CONSTRAINT]` 限制，但 LLM 并不保证遵守。恶意或被污染的模型输出可能写入包含有害命令的脚本（如 `rm -rf /`）。虽然目前这些文件仅保存不执行，但如果后续功能集成自动执行 PoC，则成为 RCE 入口。

**建议**: 
- 在写入前进行静态危险模式检测（`rm -rf`, `format`, `dd if=` 等）
- 为 `extractPocsFromText()` 添加一个独立的白名单/黑名单过滤层
- 将 PoC 写入隔离的 tmp 目录而非 `originalWorkDir/pocs`

---

## 四、🟡 Suggestion 级问题（强烈建议修复）

### 🟡 1. 错误处理大面积忽略

**遍布**: `engine.go`, `blackboard.go`, `critic.go`, `sector.go` 等

```go
_, _ = bm.AddFinding(hid, f.Title, ...)   // engine.go:431
_, _ = bm.AddHypothesis(...)               // engine.go:422
_ = bm.persistLocked()                     // blackboard.go:94
```

**影响**: 
- 如果 `AddHypothesis` 因为 "negative polarity hypothesis rejected" 返回错误，会静默丢失数据
- `persistLocked()` 失败意味着 Blackboard 状态未持久化 — 如果进程崩溃，前序工作全部丢失
- 虽然部分场景是 "best-effort"（如提示词构建时），但在 `integrateResults()` 中忽略错误可能导致发现丢失

**建议**: 根据场景分级处理：
- 对 `AddFinding`/`AddHypothesis` 等：至少 log 错误（通过 event bus）
- 对 `persistLocked()`：实现 3 次重试，失败后通过 event bus 发送错误事件
- 对 `parseAndApply()` 等 parse 类操作：忽略是合理的（LLM 输出不可控）

### 🟡 2. 代码重复：类型转换辅助函数

**文件**: `cmd/zdll/main.go:939-961` 和 `internal/core/drone.go:726-766`

`intValue()`, `floatValue()`, `stringValue()` 在两个包中**独立实现**，逻辑几乎相同但略有差异（cmd 版本的 `intValue` 少处理了 `float64` 类型作为 int 的情况）。

**建议**: 提取到 `internal/core` 或新建 `internal/util` 包，统一使用。

### 🟡 3. 缺少对 LLM JSON 输出的深度校验

**文件**: `internal/core/engine.go:395-444` (`parseAndApply()`)
**严重度**: Medium

```go
var parsed struct { ... }
if err := json.Unmarshal([]byte(jsonStr), &parsed); err == nil {
    for _, h := range parsed.Hypotheses {
        if h.Confidence > 0.15 && h.Claim != "" {
            _, _ = bm.AddHypothesis(h.Claim, h.Confidence, nil)
        }
    }
```

**问题**:
- 不校验 `h.Confidence` 上限（如 > 1.0）
- 不校验 `f.Severity` 是否为已知值（critical/high/medium/low/none）
- 大模型幻觉可能注入超长字符串（如 100KB 的 "claim"），导致内存问题

**建议**: 添加字段级约束校验，对超长字符串截断。

### 🟡 4. 硬编码版本号散落各处

- `internal/core/types.go:225`: `"HIVE-MIND INTEL ENGINE V8.0"`
- `internal/report/sarif.go:83`: `"0.1.0"`
- `cmd/zdll/main.go:79`: `"0.1.0"`

**建议**: 定义单一版本常量（如 `internal/version.go`）。

### 🟡 5. Tree-sitter Parser 未复用，存在性能浪费

**文件**: `internal/core/tree_sitter.go:196`

```go
func analyzeSource(...) []*Finding {
    parser := sitter.NewParser()  // ❌ 每个文件新建Parser
    defer parser.Close()
    parser.SetLanguage(setup.lang)
```

`go-tree-sitter` 的 `Parser` 对象在调用 `SetLanguage` 后是可复用的，但当前每个文件分析都重新创建。在扫描 500 个文件时，Parser 创建/销毁开销可观。

**建议**: 使用 `sync.Pool` 为每种语言维护 Parser 池。

### 🟡 6. Critic 模块缺少反馈闭环

**文件**: `internal/core/critic.go`

Critic 标记 duplicate 和 weak hypothesis 后，`Review()` 直接将这些假设设为 `HypothesisDiscarded`。但：
1. 不存在人工审核环节 — 如果 LLM 误判（false positive），合法假设被永久丢弃
2. 被标为 duplicate 的假设保留的 `keep_id` 未做合并 — 两个假设的 evidence/tasks 未迁移

**建议**: 
- 给 duplicate 标记添加可配置的置信度阈值，低于阈值的打 `HypothesisSuspected` 而非直接丢弃
- 合并 duplicate 的 evidence 和 tasks 到 keep 假设

### 🟡 7. Git Clone 缺少安全校验

**文件**: `internal/app/target.go:22-24`

```go
cmd := exec.Command("git", "clone", "--depth", "1", target, dest)
```

- `target` 是用户提供的 URL，直接拼接到 `exec.Command`，虽然在 POSIX 下通过参数列表传递（非 shell），但 `git clone` 本身的参数注入（如 `--config=core.fsmonitor=evil.exe`）是潜在风险
- `repoNameFromURL()` 提取的目录名未经 `../` 检查（虽然 `SanitizeFilename` 会替换 `/` 为 `_`）

**建议**: 
- 对 URL 做额外校验（如移除 `--config` 等 git 选项前缀）
- `repoNameFromURL` 显式检查不包含 `..`

### 🟡 8. Dedup 模块存在不完整逻辑

**文件**: `internal/core/dedup.go:346-353` (`isTaskDuplicate()`)

```go
func isTaskDuplicate(existing *DroneTask, newDescription string) bool {
    if existing.HypothesisID != "" && existing.DroneRole != "" {
        // simplified: same hypothesis_id and role and substring containment
        if existing.HypothesisID != "" {
            // placeholder for exact logic  ❌ 永远为true的空逻辑
        }
    }
```

内层 `if existing.HypothesisID != ""` 永远为真（外层已检查），且其 body 是注释占位符。这意味着 `isTaskDuplicate()` 的逻辑实际退化为纯文本匹配。

**建议**: 移除冗余检查，补全 hypothesis_id + role 相同的精确匹配逻辑。

---

## 五、💭 Nit 级问题（可选改进）

| # | 位置 | 问题 | 建议 |
|---|------|------|------|
| 1 | `drone.go:25` | `passthroughRoles` 是全局可变 map，无必要的全局状态 | 改为 package-level const 切片 |
| 2 | `types.go:281` | `generateID()` 使用 `crypto/rand` 每次3字节，高频调用可能耗尽 entropy | 低风险，可考虑 `math/rand/v2`（Go 1.22+） |
| 3 | `doc_intel.go:44` | `SkipDirs` 未包含 `.circleci`、`.github` 等常见 CI/配置目录 | 按需添加 |
| 4 | `sarif.go:48` | `extractLocation()` 函数被误放到 `sarif.go` 但也被 `app.go` 使用 | 提取到独立的 `location.go` |
| 5 | `config.go:133` | `kimi-for-coding` 硬编码为默认模型 | 可考虑写入文档而非代码 |
| 6 | `engine.go:121` | `exportTimeGuard()` 返回 `time.Duration` 但调用者忽略 | 移除该函数或用 `_ = ` 明确忽略 |
| 7 | `dedup.go:46-47` | `hash.Write()` 的错误返回值被忽略 | FNV hasher 的 Write 不会返回错误，但建议加 `_ =` 显式忽略 |
| 8 | `drone.go:660` | `ensureSymlink()` 在 Windows 上大概率失败（需要管理员权限）| 添加平台判断，Windows 下直接使用 `effectiveWorkDir = originalWorkDir` |
| 9 | `blackboard.go:69` | `WorkDir()` 用了 `RLock` — workDir 是只读字段，可以不需要锁 | 简化或改用 `sync.Once` |

---

## 六、架构评估

### 6.1 组件职责划分

| 组件 | 评分 | 评价 |
|------|------|------|
| Engine (Cerebrum) | ★★★★☆ | 核心编排逻辑清晰，budget/guard 设计合理。parseAndApply + integrateResults 分离得当 |
| Blackboard | ★★★★☆ | 线程安全设计好，去重管线优秀。Snapshot() 的安全性问题拉低评分 |
| Drone | ★★★☆☆ | 多轮追踪机制完善，但安全沙箱仅有符号链接级别的隔离（Windows 下失效） |
| Scanner | ★★★★☆ | tree-sitter + osv-scanner 双通道扫描覆盖全面，sink 分级合理 |
| LLM 接入 | ★★★★☆ | Kimi SDK 封装简洁，Agent YAML 动态生成实用 |
| Report | ★★★★☆ | 5 种格式覆盖完整，SARIF 2.1.0 合规，Finding Key 基线对比实用 |
| Coordinator | ★★★☆☆ | Sector 分区算法合理，但 cross-sector analysis 是占位实现，context 传播不完备 |
| CLI | ★★★☆☆ | Cobra 命令树结构好，但 main.go 过长（962行），应拆分 |

### 6.2 依赖分析

```
go.mod 核心依赖:
├── kimi-agent-sdk/go    ← 单一 LLM 厂商绑定
├── spf13/cobra          ← CLI 框架（合理）
├── spf13/viper          ← 配置管理（合理）
├── smacker/go-tree-sitter ← CGO 依赖（跨平台部署需注意）
└── fatih/color          ← 终端输出（合理）
```

**架构风险**: 当前**深度绑定 Kimi API**。虽然 `llm.Runner` 接口定义良好，`FakeRunner` 可用于测试，但要支持其他 LLM 需要实现新的 `AgentRunner`。建议 `AgentConfig` 中添加 `Provider` 字段以支持未来多 provider。

### 6.3 并发模型

项目使用了三种并发原语：
1. **`sync.RWMutex`** (BlackboardManager) — 读写锁保护共享状态
2. **`chan struct{}` semaphore** (DronePool, SessionPool, Coordinator) — 限流
3. **`sync.WaitGroup`** (DronePool) — 等待组完成

整体设计合理，但如前所述的 `Snapshot()` 返回裸指针破坏了隔离性。另外 Coordinator 中创建 Engine goroutine 后没有统一的超时/取消机制。

### 6.4 测试覆盖

| 模块 | 测试文件 | 覆盖估计 |
|------|---------|---------|
| cmd/zdll | main_test.go | 基础（CLI 标志、配置） |
| app | app_test.go | 基础（路径过滤、fake scanner） |
| config | config_test.go | 基础 |
| core/engine | engine_test.go, engine_run_test.go, engine_trace_test.go | 中等 |
| core/blackboard | blackboard_test.go | 基础 |
| core/tree_sitter | tree_sitter_test.go | 基础 |
| core/context | context_test.go | 基础 |
| scanner/git | git_test.go | 基础 |
| report/sarif | sarif_test.go, sarif_location_test.go | 中等 |
| report/finding_key | finding_key_test.go | 基础 |
| **core/dedup** | **无** | **0%** |
| **core/drone** | **无** | **0%** |
| **core/sector** | **无** | **0%** |
| **core/critic** | **无** | **0%** |
| **core/session_pool** | **无** | **0%** |
| **core/doc_intel** | **无** | **0%** |
| **core/guard** | **无** | **0%** |

**关键模块缺少测试**: dedup（去重正确性）、drone（追踪逻辑）、sector（分区正确性）、critic（审查准确度）均无测试。

---

## 七、安全性专项评估

| 检查项 | 状态 | 详情 |
|--------|------|------|
| SQL注入 | ✅ 无风险 | 不使用传统数据库 |
| 命令注入 | ⚠️ 中等 | `exec.Command` 参数化（非shell），但 `git clone` URL 未严格校验 |
| 路径遍历 | ⚠️ 低 | PoC 文件名使用 `sanitizeTaskID()`，但 `ext` 从 LLM 输出 `extensionMap` 取值，理论存在注入 |
| API密钥泄露 | 🔴 需修复 | `config validate` 可能打印非掩码密钥 |
| 供应链安全 | 🔴 需修复 | OSV-Scanner 自动下载无校验 |
| SSRF | ⚠️ 低 | `downloadOSVScanner()` HTTP 请求到固定 GitHub URL，风险可控 |
| 不安全的反序列化 | ✅ 安全 | `json.Unmarshal` 到结构体，无 `interface{}` 动态执行 |
| 文件权限 | ⚠️ 低 | 文件创建使用 `0o644`/`0o755`，合理；config 使用 `0o600` 保护私密 |

---

## 八、改进优先级与建议路线图

### 第一阶段（立即修复 — 发布前）
1. **修复 Snapshot() 数据竞争** — 影响每次扫描的正确性
2. **修复 API Key 掩码逻辑** — 安全合规
3. **添加 OSV-Scanner 下载校验** — 供应链安全

### 第二阶段（短期 — 下个迭代）
4. **为 dedup/drone/sector 添加单元测试** — 质量基础
5. **修复 Coordinator context 传播** — 资源管理
6. **统一错误处理策略** — 代码质量
7. **提取重复代码到 util 包** — 可维护性
8. **修复 dedup 中 `isTaskDuplicate` 的不完整逻辑**

### 第三阶段（中期 — 架构优化）
9. **LLM 输出深度校验层** — 鲁棒性
10. **Tree-sitter Parser 池化** — 性能
11. **解耦 LLM 提供商绑定** — 可扩展性
12. **强化 Drone 沙箱隔离** — 安全性

---

## 九、总结

zdll 是一个**架构扎实、未完成度合理的**安全分析引擎。核心的去重算法、多轮追踪机制、分区编排设计体现了扎实的工程功底。但在并发安全（数据竞争）和供应链安全（二进制下载）方面存在两个必须修复的 Blocker 问题。

**总体评分**: 7.2/10

- 架构设计: 8/10
- 代码质量: 7/10
- 安全性: 6/10
- 可测试性: 5/10
- 可维护性: 7/10

*本报告仅基于代码静态审计，未运行动态分析或渗透测试。建议在修复 Blocker 级别问题后，结合 `go test -race ./...` 进行并发安全验证。*
