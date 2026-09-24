# 可疑点登记册（suspicious registry）

> 审计/评审中发现的疑似 bug 先登记于此，修复后翻转为正式回归测试并记录 commit SHA。
> **行号为发现时坐标**（旧仓 `/home/fuzz/huntzero` phase1/main，2026-07 审计批次），
> 重建仓（2026-09）行号可能漂移；本册按 `/home/fuzz/kimiSec/.superpowers/sdd/` 幸存报告重建。

| # | 位置（发现时坐标） | 现象 | 表征/回归测试 | 状态 |
|---|---|---|---|---|
| 1 | `engine/drone.py:370` | `self._cleanup()` 缩进疑似在 chase 循环内每轮触发 | 无 | **查明**：刻意设计非 bug——缩进 12 格属 `async with` 体层级，每次 execute 仅一次；与 `_acquire_session().finally` 的调用幂等冗余，实测 fd/RSS 零增量，不建议修（investigation-drone-cleanup） |
| 2 | `server/http.py:226` `_validate_project_path` | 返回注解引用未导入的 `Path`，`create_app()` 构造期 NameError，FastAPI 无法启动 | 旧仓 `TestHttpValidateProjectPath`（xfail 转正） | **随方向关闭**：旧仓修复于 `4fbafe0`；CLI-only 方向变更后 server/ 整体删除，终态无此代码 |
| 3 | `kimi_hive.py:841,847` | FinOps 模型名常量 `kimi-long-context`/`kimi-latest` 与实际 `kimi-for-coding` 不符，成本估算失真 | 无（需完整扫描触发） | **延期** |
| 4 | `engine/cerebrum.py:3118` | `getattr(sector, 'file_count', 0) or 50`——Sector 无 `file_count` 属性，预算恒按 50 文件计算 | `tests/test_sector_budget.py::TestComputeSectorBudget::test_compute_sector_budget`（五组映射） | **已修复**（`0858527`）：提取 `_compute_sector_budget(sector.estimated_files or 50, sector.priority)` |
| 5 | `engine/cerebrum.py` `_total_tasks` | 解析器/dispatcher/auto-PoC/Devil's 多点递增，计数虚高（实测 10 vs 应 3） | `tests/test_cerebrum_parsing.py::test_parser_does_not_count_tasks`；集成测试断言 `_total_tasks == 3` | **已修复**（`0858527`）：dispatcher 单点计数（:2502），其余递增删除并补 None 检查 |
| 6 | `tools/poc_sandbox.py:193` | `create_subprocess_exec` 未接 PIPE → `stdout.decode()` AttributeError → docker 模式恒 ERROR | `tests/test_characterization.py::TestDockerSandboxPipes`（契约回归） | **已修复**（`b239e28`）：补 `stdout/stderr=asyncio.subprocess.PIPE` |
| 7 | `kimi.py:155` | feed 子命令空转占位（TODO: GitHub auto-feed） | 无 | **已处置（删除）**：旧仓 `afcfe7a` 删除；重建仓终态 `huntzero.py` 仅 `scan` 子命令，无此代码 |
| 8 | `engine/blackboard.py:730-737` | 布隆预筛对原始字符串 FNV-1a、无归一化 → 五层模糊去重退化为精确串匹配；历史 test_sector 失败根因 | `tests/test_blackboard_dedup.py::TestFuzzyDedupViaPublicApi`；历史 `test_hypothesis_dedup_within_partition` 翻绿 | **已修复**（`a4569a7`）：`BloomDeduplicator` 全量删除，无条件走相似度管线 |
| 9 | `engine/blackboard.py:749` | DISCARDED 复活分支不可达（所有层跳过 DISCARDED） | `TestMergeSemantics::test_discarded_hypothesis_revival` / `..._below_revival_threshold`（0.4/0.3 边界） | **已修复**（`a4569a7`）：状态跳过随管线统一删除，复活阈值 0.4 保留 |
| 10 | `engine/blackboard.py:1344-1378` | BlackboardPartition 分区版去重仅三层，与全局版不对称 | `TestPartitionUnifiedPipeline::test_partition_anchor_layer_merges` | **已修复**（`a4569a7`）：提取模块级 `_find_similar_in`，全局/分区统一委托 |
| 11 | `tools/exploit_analyzer.py:133` | 循环变量 `desc` 遮蔽函数参数 → CVE 提取作用于常量串，`cve_dependencies` 恒 `[]` | `tests/test_exploit_analyzer.py::test_cve_extraction_from_desc_not_code` | **已修复**（`b239e28`）：循环变量改名 `pattern_desc` |
| 12 | `engine/llm_config.py:28` | `timeout=3600.0` 被 pydantic 静默丢弃（`LLMProvider` 无该字段） | 无 | **查明**：无实际影响，不修——SDK 0.0.5 全链路无超时配置路径，实际生效 openai 默认 600s/次读（流式为静默上限）+ 两层重试 + 应用层 wait_for 多层冗余（investigation-llm-timeout） |
| 13 | `engine/semantic_helper.py`（tools 版并入处） | `call_graph --direction callees` 声明但从未实现（`callees` 列表声明后从未填充） | 无 | **已处置（删除）**：旧仓 `afcfe7a` 删除 callees 选项。⚠️ 备注：重建仓 task-14 合并复刻（`33e11c6`）将 callees 随源码回带入，仍未实现——现见于 `engine/semantic_helper.py:722`/`:1156`，待复查是否按旧仓终态再删 |
| 14 | `kimi.py` `cmd_binrev` | binrev 子命令 lazy-import 未入库的外部 `binrev` 包，调用必失败 | 无 | **已处置（删除）**：旧仓 `afcfe7a` 删除；重建仓终态 `huntzero.py` 仅 `scan` 子命令，无此代码 |
| 15 | `engine/cerebrum.py:348` | TerminationGuard 注释称"L5 由外部 asyncio.wait_for 保证"，实际 4 个 run 入口均无外部 wait_for | 无 | **查明**（注释修正 `aa1bb8d`）：L5 实为传输层兜底——httpx 读超时 600s/次 × SDK 重试，挂死请求有界；L1 在解析器 |
| 16 | —（条目内容未能从幸存报告恢复） | — | — | **随方向关闭**（CLI-only 终态不含相关代码） |

## 状态统计

- 查明：#1、#12、#15
- 延期：#3
- 已修复（重建仓 commit）：#4、#5（`0858527`）、#6、#11（`b239e28`）、#8、#9、#10（`a4569a7`）
- 已处置（删除）：#7、#13、#14（#13 在重建仓有回带入残留，见条目备注）
- 随方向关闭：#2、#16
