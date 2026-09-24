# Rebuild Stage B 报告：引擎登记册修复复刻（8 项 bug + semantic_helper 合并）

- 日期：2026-09-25
- 仓库：/home/fuzz/huntzero（main，基线 HEAD 36a4729，工作区干净）
- 依据报告：`/home/fuzz/kimiSec/.superpowers/sdd/fix-batch{1,2,3}-report.md` + `task-14-report.md`
- 结果：基线 **16 passed / 1 failed** → 现状 **17 passed / 0 failed**，验收点翻绿，ruff 零新增（task-14 报告明文接受的逐字移植增量除外）

## Commits（按批次）

| Commit | 内容 | 对应被删仓 commit |
|---|---|---|
| `a4569a7` | fix(blackboard): remove bloom prefilter, enable DISCARDED revival, unify dedup pipeline (registry #8/#9/#10) | 454aced |
| `0858527` | fix(cerebrum): repair sector budget field and single-point task counting (registry #4/#5) | 6cedd96 |
| `b239e28` | fix(tools): unshadow desc in exploit_analyzer and pipe docker sandbox output (registry #11/#6) | 98f2452 |
| `33e11c6` | refactor(tools): merge tools/semantic_helper into engine version (SPEC 5.1) | 17efe1f |
| `aa1bb8d` | docs(cerebrum): correct TerminationGuard L5 comment to transport-level backstop (registry #15) | —（原仓未含，按 brief 第 6 项落地） |

## 每处改动与报告的对应性

### 批次 1 — engine/blackboard.py（+105 / −233，报告口径 +120/−224 含测试文件；本仓无 test_blackboard_dedup.py，差异即该测试文件）

- **#8**：`BloomDeduplicator` 类（含节注释）整体删除；`Blackboard._bloom` 与 `BlackboardPartition._bloom` 初始化删除；两类 `add_hypothesis` 的布隆预筛分支删除，现为无条件 `existing_id = self._find_similar_hypothesis(description)`；两处 `self._bloom.add(description)` 删除。全仓 `rg -ni '_bloom|BloomDeduplicator|bloom' --type py` 零残留（与批次 1 报告证据一致）。
- **#9**：全局管线三处 `if h.status == HypothesisStatus.DISCARDED: continue` 随 #10 提取一并删除，新 `_find_similar_in` 函数体内无任何状态跳过。
- **#10**：模块级 `_find_similar_in(hypotheses, description, threshold=0.55)` 提取，位于前类结束之后、`Blackboard` banner 之前（行号算术与报告 post-commit :340 / :659 / :624-625 精确对齐）；`self.hypotheses`→参数、`self._normalize_for_dedup`→`Blackboard._normalize_for_dedup`（4 处）、`self._char_trigrams`→`Blackboard._char_trigrams`（2 处）；两类 `_find_similar_hypothesis` 均为 2 行委托；分区三层精简版删除；docstring 保留"四层级"原文（纯移动纪律，同报告）。
- **语义数字零改动**（grep 验证）：merge boost `min(0.05, 0.02*n)`（:657/:1216）、复活阈值 0.4（:664/:1221）、Layer 阈值 0.30/+0.10/0.70（:379/:416/:432）。

### 批次 2 — engine/cerebrum.py（+29 / −20）

- **#4**：`_compute_sector_budget(file_count, priority)` 提取于 `TerminationGuard` 结束与 `Cerebrum` banner 之间（:437-445）；公式逐字 `max(40,min(120,files*0.8))*multiplier`、`max(10,min(25,tasks//4))`、`{0:1.5,1:1.2,2:1.0,3:0.8}.get(priority,1.0)`；调用点 `getattr(sector,'file_count',0) or 50` → `_compute_sector_budget(sector.estimated_files or 50, sector.priority)`（`Sector.estimated_files: int = 0` 核实于 sector.py:56）。
- **#5**：`_total_tasks += 1` 现仅存于 `_drone_dispatcher`（**:2502**，与报告 post-commit 行号一字不差）；JSON 解析（既有 None 检查不动）、XML 解析、auto-PoC、Devil's 四处递增删除；XML/auto-PoC/Devil's 三处补 `if t_id:` / `if poc_task_id:` / `if falsify_task_id:` None 检查（去重拦截时不再 `put(None)`、不发事件；XML 检查与 JSON 路径对称含同款注释）。

### 批次 3 — tools/exploit_analyzer.py + tools/poc_sandbox.py（+6 / −3）

- **#11**：:133 `for pattern, pattern_desc in _ENV_CONFIG_PATTERNS:`，循环体 `env_configs.append(pattern_desc)`。功能验证：`_static_analysis("see CVE-2024-12345 details", code="patch mentions CVE-1999-99999")["cve_dependencies"] == ["CVE-2024-12345"]`（desc 提取、code 不提取），与批次 3 报告翻转后断言一致。
- **#6**：docker 模式 `create_subprocess_exec` 补 `stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.PIPE`，每行一个 kwarg，与 LocalSandbox(:86-92) 风格一致。

### task-14 — engine/semantic_helper.py（+640 / −632 含 git rm）

- 并入 `LANG_MAP`/`LANG_EXTENSIONS`、`_load_language`、`_collect_files`、`_parse_source`（tools `_parse_file` 改名，避开 engine 版同名异签名函数）、`cmd_ast_query`/`cmd_call_graph`/`cmd_scope_extract`/`cmd_data_flow`、`_get_callers_with_context`/`_find_call_sites`/`_find_enclosing_function`/`_trace_backward`；`_run_analysis_command(cmd, argv)` 适配层保留手写分发结构，argparse 选项 `-t/-l/-q/-f/-n/-s/-k/-d` 逐字；usage 文本 +4 行；模块级补 `import json`/`import sys`（isort 序）；`git rm tools/semantic_helper.py`。
- **3 处最小修复**（均带代码注释，同报告）：`_ts_query`/`_ts_captures`（0.26 API `tree_sitter.Query(lang,q)` + `QueryCursor(q).captures(node)`，旧 API 存在时走旧路径——本环境 0.26.0 实测 `hasattr` 分支判定正确）；`_find_call_sites`/`_find_enclosing_function` 显式 `language` 形参（原版 NameError 被静默吞掉）；`_trace_backward` 基例 `return [current_path]`。
- **golden 证据**：engine 版 4 个既有子命令在 /tmp/fixture_proj（原 fixture_proj 复刻）上 HEAD vs 合并后 `diff` 逐字节一致（list_functions / find_references / get_call_graph / get_function_body 全静默通过）。新子命令功能验证：`ast_query` 捕获 parse_header count=1；`call_graph -f parse_header` 返回定义点；`scope_extract -n parse_header` 提取 1-2 行；`data_flow -s entry -k dangerous` 返回 `dangerous -> mid -> entry` count=1 **无尾部重复**；`--help` 含 `data_flow`。
- 残留引用：仅 docs/superpowers（历史方案，同报告豁免）与 engine 版 2 处有意保留的出处注释；`.bots.md`/README 无 tools 版引用。

### registry #15 — engine/cerebrum.py:348（+1 / −1）

- L5 行改为"传输层兜底：httpx 读超时（600s/次）×SDK 重试（APITimeoutError → 主循环 break → 轮边界 L4 判定），挂死请求有界（最坏 ~数十分钟），非逐轮硬墙"，逐字按 brief。

## 测试与 lint 证据

- **验收点**：`pytest tests/test_sector.py::TestBlackboardPartition::test_hypothesis_dedup_within_partition` → **1 passed**（基线 FAILED，批次 1 后翻绿；tests/test_sector.py 未改一行）。
- **全量套件**：每个 commit 后 `pytest tests/ -q` 均 **17 passed / 0 failed**（基线 16 passed / 1 failed）。
- **ruff（与各自 HEAD 基线逐条 diff）**：
  - blackboard.py：基线 5 F541 + 1 I001 + 2 N806 + 2 W293 → 现少 2 N806（随 BloomDeduplicator 删除），零新增——与批次 1 报告完全一致。
  - cerebrum.py：基线 74 条 → 批次 2 后与批次 3/#15 后均 74 条，去行号排序 diff 为空（IDENTICAL_RULE_SET）——零新增。
  - exploit_analyzer.py / poc_sandbox.py：基线与现状 IDENTICAL——零新增。
  - semantic_helper.py：基线 6（5 UP045 + 1 I001）→ 现 9（+2 UP045 来自逐字移植的 `Optional` 注解、+1 F841 `callees` 未用变量，均为 tools 版原文）——task-14 报告明文接受的增量（原仓 +3 UP045/+1 F841；本复刻无 F401：两个 finder 内经 `_ts_query` 路由后死掉的 `import tree_sitter` 已随修复 #2 一并移除，原仓 10 条中同样无 F401 可证）。

## Files changed

- `engine/blackboard.py`（a4569a7）
- `engine/cerebrum.py`（0858527 批次 2；aa1bb8d 注释）
- `tools/exploit_analyzer.py`、`tools/poc_sandbox.py`（b239e28）
- `engine/semantic_helper.py`（33e11c6）、`tools/semantic_helper.py`（git rm，33e11c6）

## 与报告的差异 / 关注项

1. **测试文件未复刻**：原三批报告含 tests/ 改动（test_blackboard_dedup.py、test_sector_budget.py、test_cerebrum_parsing.py 等），本仓 tests/ 仅 test_env_sync.py + test_sector.py，被删仓的测试基线不存在，按"源码忠实复刻 + 既有验收点翻绿"口径执行，未新建测试文件（#11/#6/#10 的回归断言已用等值功能验证替代并记录于上文）。
2. **task-14 合并后行数 1215（原仓 1183）**：差异来自适配层与出处/修复注释的写法细节，无功能差异；4 个既有子命令 golden 逐字节一致为证。
3. 原批次 2 报告披露的成本语义变化（`_total_tasks` 只计 dispatcher 实际派发、大 sector 预算最高 180/25）同样适用于本复刻，属方案内预期。
4. 真实 docker 端到端验证未做（同原报告：以契约为准，留待 VM 环境）。
