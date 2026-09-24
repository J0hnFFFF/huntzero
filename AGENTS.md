# Repository Guidelines

huntzero — LLM 主导的假设驱动漏洞挖掘引擎（纯 CLI，Proprietary）。

## Project Structure & Module Organization

- `huntzero.py`：统一 CLI 入口，唯一子命令 `scan`，委托 `kimi_hive.run`。
- `kimi_hive.py`：扫描主流程（会话编排、预算控制、报告导出）。
- `kimi_sdk_compat.py`：kimi-agent-sdk 兼容补丁。
- `engine/`：引擎核心——`cerebrum`（主脑编排）、`drone`（工蜂执行）、`blackboard`（共享状态/去重）、`sector`（目标分区）、`backends`（存储）、`llm_config`（LLM 配置）、`osv_bridge`（依赖漏洞桥）、`semantic_helper`（AST 导航 CLI）、`session_pool`、`fuzzer`。
- `skills/`：领域 playbook，按目标类型加载（小写 kebab 目录名）。
- `tools/`：`check_env_sync.py`（env 同步门禁）、`install_osv.py`、`poc_sandbox.py`、`exploit_analyzer.py`、`finops_monitor.py`、`build_cython.sh`（Cython 编译管线）。
- `tests/`：pytest 套件 + `fake_llm.py` 集成 harness + `fixture_proj/`。
- `docs/`：`design/` 设计文档、`suspicious-registry.md` 疑似 bug 登记册、`cython-spike.md` 编译结论。
- 运行时产物（`local_workspace/`、`tmp/`、reports、`.blackboard*.json`、`.audit_notes.md`、`build/` 等）不入库。

## Build, Test, and Development Commands

- `pip install -e ".[dev]"`：安装可编辑包（含 `huntzero` console script）与 pytest/black/ruff/mypy。
- `python -m pytest`：全量测试；`python -m pytest tests/test_example.py` 跑单文件。
- `python tools/check_env_sync.py`：环境变量与 `.env.example` 同步检查。
- `bash tools/build_cython.sh`：Cython 编译 engine//tools/ 到 `build/compiled-pkg/`。
- `export KIMI_API_KEY=sk-... && huntzero scan <target>`：运行扫描（等价 `python huntzero.py scan <target>`）。

## Coding Style & Naming Conventions

Python 3.11+，四空格缩进。`black` 100 列；`ruff` 规则集 `E,F,W,I,N,UP`（忽略 `E501`，配置见 `pyproject.toml`）。模块/函数/变量 `snake_case`，类 `PascalCase`。改动文件 ruff 不得新增告警（与 HEAD 基线比对）；存量格式债不顺势偿还。

## 环境与配置约定

- `KIMI_*` 环境变量名不动（SDK 契约）；`kimi-for-coding`（模型名）、`kimi_agent_sdk`/`kimi_cli`/`kosong`（SDK 包名）、`api.kimi.com`（端点）不改。
- `kimi_hive.py` 文件名保留；`skills/kimisec-hive/` 目录名保留。
- 新增环境变量必须同步 `.env.example`（`tests/test_env_sync.py` 门禁，`python tools/check_env_sync.py` 本地预检）。
- 依赖以 `pyproject.toml` 为唯一权威。

## Testing Guidelines

- `pyproject.toml` 配置 pytest（`tests/`、`test_*.py`、pytest-asyncio auto mode）。基线 100 passed / 0 failed，任何改动不得退化。
- 审计/评审中发现的疑似 bug 先登记 `docs/suspicious-registry.md`（含位置、现象、表征测试、状态），修复后翻转为正式回归测试并在登记册记录 commit SHA。
- 集成测试 harness 路由依赖 prompt 字面子串（`CRITIC AGENT`、`[DRONE TASK:`），改引擎 prompt 文案时同步 `tests/fake_llm.py`。

## Commit & Pull Request Guidelines

- Conventional Commits（如 `fix(blackboard): ...`、`docs: ...`、`refactor(brand): ...`），主题拆分，单 commit 不夹带无关文件。
- 提交前跑全量 pytest 与涉及文件的 ruff 基线比对；PR 说明含动机、执行过的命令、环境/配置变更。
- 永不提交 `.env`、API key、HMAC secret、浏览器会话、本地工作区与生成的 blackboard/audit 产物。
