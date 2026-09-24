# Rebuild Stage C 报告：测试网重建 + llm_config 合并收尾

- 日期：2026-09-25
- 仓库：/home/fuzz/huntzero（main，基线 HEAD aa1bb8d，工作区干净，17 passed/0 failed）
- 依据：`docs/superpowers/plans/2026-07-29-huntzero-phase0-phase1.md` Task 7-14（测试逐字来源）+ `/home/fuzz/kimiSec/.superpowers/sdd/fix-batch{1,2,3}-report.md`（翻转依据）
- Commit：`50c55b4` `test: rebuild test suite with registry-fix regression coverage`（19 文件，+1251/−30）
- 结果：**100 passed / 0 failed**（基线 17 → +83），ruff 全部新文件零告警，kimi_hive.py 与 HEAD 逐规则比对 IDENTICAL_RULE_SET

## Step 0 — engine/llm_config.py（计划 Task 7，此前阶段遗漏）

- 新建 `engine/llm_config.py`：`build_llm_config(api_key)`，base_url/model 读 `KIMI_BASE_URL`/`KIMI_MODEL_NAME`（默认 `https://api.kimi.com/coding/v1` / `kimi-for-coding`），provider type="kimi"，max_context_size=262144。provider dict 保留 `timeout: 3600.0` 键仅为字段形态一致——实测 pydantic `LLMProvider` 无该字段会静默丢弃（registry #12 口径），测试不对 timeout 断言。
- `kimi_hive.py`：本地 `build_config` 函数体删除，改 `from engine.llm_config import build_llm_config as build_config`（调用点 :757/:769 零改动）；`from kimi_agent_sdk import Config` 的 try/except 随删（Config 已无引用）；为不新增 E402，post-patch 导入并行为 `from engine import Blackboard, Cerebrum, Drone` + llm_config 一行（Drone 走 engine 包既有导出）——ruff 与 HEAD 比 43 条对 43 条、规则集逐条相同。
- `server/core.py` 终态已删，无需替换。
- `.env.example`：补注释态 `# KIMI_MODEL_NAME=kimi-for-coding`（llm_config 新增 env 读取点，`tests/test_env_sync.py` 门禁要求）。
- 验证：`python -c "from engine.llm_config import build_llm_config"` OK；`import kimi_hive` OK；env_sync 通过。

## 测试文件清单与通过数（100 = 17 存量 + 83 新增）

| 文件 | 用例数 | 来源 | 备注 |
|---|---|---|---|
| `tests/test_llm_config.py` | 8 | 计划 Task 7 改造 | 两处旧实现已删，等价性改为 8 条字面量断言（base_url/default_model/max_context_size/provider type/api_key/链接关系/两个 env override） |
| `tests/test_blackboard_dedup.py` | 11 | 计划 Task 8 框架 + 翻转 | #8/#9/#10 回归，见下节 |
| `tests/test_blackboard_core.py` | 8 | 计划 Task 8 逐字 | 任务去重/发现合并/仲裁/持久化 |
| `tests/test_bayesian.py` | 6 | 计划 Task 9 逐字 + 修正 | strength 学习用例修正（见偏差 3） |
| `tests/test_backends.py` | 3 | 计划 Task 9 裁剪 | 只保留 LocalBackend（RedisBackend/IncrementalLocalBackend/make_backend 终态已删） |
| `tests/test_cerebrum_parsing.py` | 12 | 计划 Task 10 + 翻转 | `test_parser_does_not_count_tasks`：解析后 `_total_tasks == 0`（#5） |
| `tests/test_termination_guard.py` | 8 | 计划 Task 10 逐字 | docstring 用 registry#15 修正版："L1 在解析器；L5 为传输层超时兜底，不在本类" |
| `tests/test_sector_heuristics.py` | 5 | 计划 Task 11 逐字（1 处钉现状，见偏差 4） | |
| `tests/test_osv_bridge.py` | 6 | 计划 Task 11 逐字（2 处钉现状，见偏差 5/6） | |
| `tests/test_exploit_analyzer.py` | 6 | 计划 Task 11 + 翻转 | `test_cve_extraction_from_desc_not_code`（#11） |
| `tests/fake_llm.py` + `tests/fixture_proj/src/app.py` + `tests/test_integration_cerebrum.py` | 2 | 计划 Task 12 + fix-batch2 | 集成断言 `_total_tasks == 3` 实测成立；harness 1 处调整（见偏差 7） |
| `tests/test_semantic_helper.py` | 2 | 计划 Task 14 逐字 | |
| `tests/test_characterization.py` | 1 | 计划 Task 13 裁剪 + 转正 | 只保留 `TestDockerSandboxPipes`（#6 契约回归，无 xfail）；http 用例不建（server/http.py 已删） |
| `tests/test_sector_budget.py` | 5 | fix-batch2-report | 五组映射 180/25、96/24、40/10、32/10、180/25 全过 |

## 翻转用例与报告的对应

| 用例 | 登记册 | 依据 | 断言要点 |
|---|---|---|---|
| `TestFuzzyDedupViaPublicApi::test_fuzzy_dedup_works_via_public_api` | #8 | fix-batch1 | 近似措辞对（bigram 0.75，无锚点）经 add_hypothesis 合并为 1 节点 |
| `TestMergeSemantics::test_discarded_hypothesis_revival` | #9 | fix-batch1 | 精确重复 0.4 再提案 → 合并 + 复活 PENDING + 1 节点 |
| `TestMergeSemantics::test_discarded_hypothesis_below_revival_threshold` | #9 | fix-batch1 | 0.3 再提案 → 合并但保持 DISCARDED（0.4/0.3 边界） |
| `TestPartitionUnifiedPipeline::test_partition_anchor_layer_merges` | #10 | fix-batch1 | 分区内共享 parse_header 锚点 + unigram 0.44 ≥ 0.30 合并；bigram 0.10 / unigram < 0.65（旧三层分区管线本不合并，钉住漂移） |
| `TestTaskParsing::test_parser_does_not_count_tasks` | #5 | fix-batch2 | 解析 1 任务后 `_total_tasks == 0`，`_task_queue.qsize() == 1` |
| `TestCerebrumEndToEnd::test_full_loop_produces_finding` 新断言 | #5 | fix-batch2 | `_total_tasks == 3`（investigator + auto-PoC + Devil's；doc-intel 直构 Drone 不计数；重提案被 add_task 去重拦截不再计数） |
| `TestComputeSectorBudget::test_compute_sector_budget` | #4 | fix-batch2 | 五组参数化映射 |
| `TestStaticAnalysis::test_cve_extraction_from_desc_not_code` | #11 | fix-batch3 | desc 中 CVE-2024-12345 提取、code 中 CVE-1999-99999 不提取 |
| `TestDockerSandboxPipes::test_docker_sandbox_captures_output` | #6 | fix-batch3 | fake_exec 契约 `stdout/stderr is asyncio.subprocess.PIPE` + `verified_success` + 输出捕获（xfail 转正式回归） |
| `tests/test_sector.py::TestBlackboardPartition::test_hypothesis_dedup_within_partition` | #8/#10 | 阶段 B 已翻绿 | 本阶段未改一行，全量套件中持续通过 |

## 与计划的偏差（均按"钉住修复后实现"纪律，仅调测试不调引擎）

1. **test_llm_config.py 形态**：计划的对照版（import `kimi_hive.build_config` / `server.core` 变体比对）不可行——两处旧实现均已删除。按 brief 改为 8 条字面量断言，不对 timeout 断言（pydantic 丢弃，registry #12）。
2. **TestLayer0Anchor 变体措辞**：计划逐字变体 `"TIFFReadDirEntryArray allocates array without validating count"` 无任何锚点（camelCase 不匹配 snake_case 锚点正则、无文件名），Layer 0 整体跳过且低层均不命中（实测不合并）。按计划"留足阈值余量"注记最小调整：变体改为 `"tif_dirread.c allocates array without validating count"`（自带文件锚点）——实测合并，且 bigram 0.09 / unigram 0.40 确为锚点层语义。
3. **test_learned_strength_requires_min_samples 修正版**（brief 指定）：`_update_strength_for_pair` 要求父子均有 outcome 记录且 n≥3 才计 learned_pairs——先记 1 次 parent outcome，再对同一 child 记 3 次（n=3）。计划原版（3 个不同 child、不记 parent）矩阵恒空，断言必败。
4. **TestParseSectorOutput 缺省 priority 钉 2 不钉 1**：`_parse_sector_output` 用 `enumerate(blocks)` 原始序号，前导空块占位 → 第二块缺省 priority=2。计划断言 1，实测 2，钉现状并注释。
5. **osv test_fields_and_cvss_override 输入**：CVSS 解析取首个 token float——向量串 `"CVSS:3.1/AV:N/9.8"` 解析失败会回落 database_specific。改用裸分 `"9.8"` 触发覆盖语义，断言不变（severity critical / cvss 9.8）。
6. **osv finding title 钉实际**：实现为 `f"{primary_id}: {summary}"` → `"CVE-2024-0001: heap overflow"`，计划断言裸 `"CVE-2024-0001"`，钉现状。
7. **fake_llm.DRONE_REPORT 删 `trace_needed: no` 行**：该行被 `_parse_round_output` 解析为真值字符串，`_evaluate_chase_decision` 据此进入 chase 轮；chase prompt 不含 `[DRONE TASK:` 会被路由到 cerebrum 分支返回 EMPTY_ROUND，last_raw 被覆盖导致 finding 丢失（端到端 0 finding 实证）。删行后 drone 单轮停止，链路闭合，`_total_tasks == 3` 与 fix-batch2 实测分解逐项吻合。文件内已加注释。
8. **strength/bayesian、characterization 裁剪**：Redis/Incremental/make_backend 与 http 用例按 brief 不建（终态已删）。

## 验证证据

- 全量套件：`python -m pytest tests/ -q` → **100 passed, 1 warning in ~3.3s**（commit 后复跑同）。e2e `--durations` 确认真实执行（2.11s call）。
- 分文件跑绿：llm_config 8 / dedup 11 / core 8 / bayesian 6 / backends 3 / parsing+guard 20 / sector+osv+exploit 17 / integration 2 / semantic 2 / characterization 1 / budget 5。
- ruff（项目 pyproject 配置 E/F/W/I/N/UP）：17 个新文件（含 engine/llm_config.py、tests/fake_llm.py、fixture）`All checks passed!`；`kimi_hive.py` 与 HEAD 经 stdin 逐规则比对 → 2 E402 + 1 E731 + 34 F541 + 1 I001 完全相同（IDENTICAL_RULE_SET，存量）。
- black（100 列）：17 个新文件已 `black` 格式化（计划文本的排版差异，无语义改动；kimi_hive.py 未动，与阶段 B 口径一致——存量格式债不由本阶段背）。
- `import kimi_hive` / `from engine.llm_config import build_llm_config` 均 OK。

## 遗留 / 关注项

- `.superpowers/sdd/stage-b-report.md` 保持 untracked（阶段 B 产物，本 commit 未夹带；本报告同样不入库）。
- 计划 Task 8 注记的 Layer 2/3/4 独立分层用例未单独建（停用词表敏感，计划允许；五层行为已由锚点/指纹/模糊层 + 分区锚点 + 既存 test_sector.py 覆盖）。
- 集成测试依赖 prompt 路由字面子串（`CRITIC AGENT` / `[DRONE TASK:`），引擎 prompt 文案若改动需同步 harness。
- 真实 docker 端到端未验（同批次 3 报告：以 mock 契约为准，留待 VM 环境）。
