# Rebuild Stage D 报告：品牌统一 + README/AGENTS 重写 + Cython 管线与登记册重建

- 日期：2026-09-25
- 仓库：/home/fuzz/huntzero（main，基线 HEAD `50c55b4`，工作区干净，100 passed/0 failed）
- 参考：`/home/fuzz/kimiSec/.superpowers/sdd/task-15-report.md`（task-16-report 未幸存，build_cython.sh 按 brief 描述的最终形态 + fix-r7/final-fix 报告细节重建）
- 结果：**100 passed / 0 failed 保持**，双跑一致，ruff 零新增

## Commits

| SHA | 内容 |
|---|---|
| `dd80094` | refactor(brand): rename kimi.py to huntzero.py and unify branding（22 文件，+39/−39，rename 96%） |
| `2b7817f` | build(tools): add cython compile pipeline with dual-run verification and rebuild registry（3 文件，+227） |
| `35312ab` | docs: rewrite README and AGENTS for CLI-only end state（2 文件，+85/−385） |

commit 顺序将 cython+登记册置于 README/AGENTS 之前，使 README 引用的
`tools/build_cython.sh`、`docs/cython-spike.md`、`docs/suspicious-registry.md`
在每个 commit 上均真实存在。

## 1. 品牌替换清单与边界证据

改名/引用：`git mv kimi.py huntzero.py`；pyproject `[project.scripts] huntzero = "huntzero:main"`、
`py-modules "kimi"→"huntzero"`；huntzero.py docstring 自引用 + `prog="huntzero"`；
tools/install_osv.py docstring 示例。

品牌字符串（kimiSec/KimiSec→huntzero，HIVE-MIND/Hive-Mind→HUNTZERO/huntzero，相邻重复手工归并）：
kimi_hive.py 4（docstring 横幅、报告 engine 键、Rich Banner、argparse description；`prog="kimi_hive"` 保留）、
.bots.md 2（标题/自称，Kimi 2.5 模型名保留）、engine/__init__.py 1、engine/blackboard.py 2
（docstring + `# 🔴 HUNTZERO CONFIRMED FINDINGS`）、engine/cerebrum.py 3（CRITIC prompt 自称 +
2 注释）、engine/osv_bridge.py 1、tools/finops_monitor.py 2、tools/install_osv.py 1、
skills 13 文件（8×「作为 KimiSec」+ kimisec-hive/SKILL.md 6 + 3×report-generator 落款；
frontmatter `name: kimisec-hive` 随目录名保留）。

边界证据（替换后复跑）：

```
git diff 中 KIMI_* 行改动：0
git diff 中 kimi_agent_sdk/kimi-for-coding/api.kimi.com/kimi_cli/kosong 行改动：0
保留项：kimi_hive.py 文件名、logger 名（__name__ 体系）、kimi_sdk_compat.py 内部
  patch flag（_kimisec_patched 等）、skills/kimisec-hive 目录与 frontmatter、
  engine/fuzzer.py:54 kimisec-fuzzer:latest 镜像标签（内部基础设施标识，
  同旧仓 container_name 保留逻辑）、engine/cerebrum.py:1163 注释（指 skills 目录名）
跟踪文件品牌残留：仅 .superpowers/sdd/stage-a-report.md（历史报告）+ 上述保留项
```

集成测试安全性：fake_llm 路由子串 `CRITIC AGENT`/`[DRONE TASK:` 不受
`KimiSec CRITIC AGENT`→`huntzero CRITIC AGENT` 影响（100 passed 实证）。

验证：`pip install -e ".[dev]"` 后 `huntzero --help` 与 `python huntzero.py --help`
均输出 `usage: huntzero [-h] {scan} ...`；pytest 100 passed（test_env_sync 通过 =
未误改 KIMI_*）；改动 py 文件 ruff HEAD vs 现状逐文件计数全等（零新增）。

## 2. Cython 管线与双跑

`tools/build_cython.sh`：`set -euo pipefail`；预检 `command -v $PY/$CC` +
`$PY -c "import Cython"`（exit 2）；逐模块 `$PY -m cython --module-name <pkg>.<mod> -3`
→ `gcc -shared -fPIC -O2 -I<sysconfig include>` → `build/compiled-pkg/`；
`engine/__init__.py` 只复制；`engine/semantic_helper.py` 编为 `engine.semantic_helper_core`(.so)
+ 薄启动器（importlib 按路径直载同目录 .so，不触发 engine 包导入——避免 authlib
deprecation 告警污染，无 PYTHONPATH 可跑；分发逻辑与源码 __main__ 块逐字一致）。

环境：Python 3.13.0 / Cython 3.2.9 / gcc 13.3.0。产物 4.6M：engine 10 .so + __init__.py
+ launcher，tools 5 .so（gitignored）。

双跑（终态 HEAD 复验）：

```
源码版  python -m pytest tests -q                       → 100 passed, 1 warning in 3.07s
编译版  cd /tmp && PYTHONPATH=build/compiled-pkg:. pytest tests -q → 100 passed, 1 warning in 3.17s
加载证据 engine.{blackboard,cerebrum}/tools.poc_sandbox __file__ → *.cpython-313-x86_64-linux-gnu.so
薄启动器 7 组子命令（list_functions/find_references/get_call_graph/get_function_body/
  call_graph/data_flow/ast_query）stdout+exit code 与源码版 IDENTICAL，stderr 0 行
```

## 3. 登记册重建（docs/suspicious-registry.md）

16 项按幸存报告重建，文件头注明「行号为发现时坐标」：
查明 #1/#12/#15（#15 注释修正 aa1bb8d）；延期 #3；已修复（新 SHA）
#4/#5=0858527、#6/#11=b239e28、#8/#9/#10=a4569a7；删除处置 #7/#13/#14；
随方向关闭 #2（旧仓 4fbafe0 修复后 server/ 删除）/#16（内容未能从幸存报告恢复，如实标注）。

⚠️ **#13 偏差**：brief 口径「终态代码已删」，但重建仓 task-14 合并复刻（33e11c6）
将未实现的 `callees` 选项随源码回带入——现见于 `engine/semantic_helper.py:722`（声明未用）
与 `:1156`（argparse choices）。登记册条目已加备注，本阶段未改代码（超出 stage D 范围），
建议后续按旧仓 afcfe7a 形态补删（2 行 + usage 文本）。

## 4. README/AGENTS 重写

README：定位（LLM 主导假设驱动漏洞挖掘，纯 CLI）、Proprietary、四角色架构表 +
目录职责、开发环境（pip install -e ".[dev]"/pytest/check_env_sync/build_cython）、
配置面 5 变量表、运行（KIMI_API_KEY + huntzero scan 常用参数）、测试体系
（100 测试、fake-LLM 集成、登记册约定）。AGENTS：结构/命令/约定（black 100 列、
ruff 零新增、KIMI_* 与 SDK 名不动、疑似 bug 进登记册、prompt 路由子串同步、
conventional commits）。引用路径逐一 `[ -e ]` 核验无 MISSING。

## 5. 遗留 / 关注项

1. #13 callees 回带入（见 §3），待裁决。
2. `skills/kimisec-hive/SKILL.md:17` mcp_server.args 指向已不存在的 `kimi_mcp_server.py`
   （CLI-only 终态无 serve 子命令，旧仓 4fbafe0 的修法不适用）；该 skill 声明整体
   为 legacy 形态，待业务裁决，未动。
3. `huntzero.egg-info` 需重装刷新（已重装验证）。
4. `.superpowers/sdd/stage-{b,c,d}-report.md` 保持 untracked（阶段产物惯例）。
5. task-16-report 未幸存，build_cython.sh 按 brief 形态描述重建，双跑一致即功能等价证据。
