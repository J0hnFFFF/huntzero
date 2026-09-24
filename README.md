# huntzero

**LLM 主导的假设驱动漏洞挖掘引擎，纯 CLI 交付。**

huntzero 以 LLM 为绝对主导：引擎不使用预定义漏洞清单，而是从第一性原理出发，
围绕三类异常做假设驱动的自主推理——

- **逻辑矛盾（Logical Contradictions）**：系统声称性质 X，代码却呈现 ¬X
- **状态异常（State Anomalies）**：合法操作序列抵达未定义/非预期状态
- **越界访问（Unbound Resource Access）**：主体可引用声明作用域之外的资源

License: **Proprietary**（见 `pyproject.toml`）。

## 架构

四个核心角色，全部围绕黑板（Blackboard）解耦：

| 角色 | 模块 | 职责 |
|---|---|---|
| **Cerebrum**（主脑） | `engine/cerebrum.py` | 全局编排：提出/裁决假设、解析产出、派发任务、终止守卫（TerminationGuard）、L5 对抗反思（CRITIC） |
| **Drone**（工蜂） | `engine/drone.py` | 执行具体微任务（调查、取证、语义分析），多轮 chase 直至证据闭合或预算耗尽 |
| **Blackboard**（黑板） | `engine/blackboard.py` | 唯一共享状态中心（SSOT）：假设树四层级语义去重、任务队列、Findings 汇总与持久化 |
| **Sector**（分区） | `engine/sector.py` | 大型目标的目录级分区：独立预算、隔离分区视图、跨分区协同 |

目录职责：

- `huntzero.py` — 统一 CLI 入口（唯一子命令 `scan`，委托 `kimi_hive.run`）
- `kimi_hive.py` — 扫描主流程（会话编排、报告导出、子命令实现）
- `kimi_sdk_compat.py` — kimi-agent-sdk 兼容补丁
- `engine/` — 引擎核心（上述四角色 + `backends` 存储、`llm_config` LLM 配置、`osv_bridge` 依赖漏洞桥、`semantic_helper` AST 导航、`session_pool` 会话池、`fuzzer` 模糊测试沙盒）
- `skills/` — 领域 playbook（按目标类型加载：`web`/`binary`/`supply-chain`/`ai-agent` 等 14 个）
- `tools/` — 辅助脚本（`check_env_sync.py` 环境变量同步检查、`install_osv.py`、`poc_sandbox.py`、`exploit_analyzer.py`、`finops_monitor.py`）
- `tests/` — 测试套件（含 `fake_llm.py` 假 LLM 集成测试 harness 与 `fixture_proj/`）
- `docs/` — 设计文档（`docs/design/`）、登记册（`docs/suspicious-registry.md`）、编译 spike（`docs/cython-spike.md`）

## 开发环境

```bash
pip install -e ".[dev]"     # 安装可编辑包 + pytest/black/ruff/mypy
python -m pytest            # 全量测试（见「测试体系」）
python tools/check_env_sync.py   # 环境变量与 .env.example 同步门禁
bash tools/build_cython.sh       # Cython 编译（见 docs/cython-spike.md）
```

配置面（`.env.example` 为模板，新增变量必须同步——`tests/test_env_sync.py` 强制）：

| 变量 | 必需 | 说明 |
|---|---|---|
| `KIMI_API_KEY` | ✅ | LLM 认证 |
| `KIMI_BASE_URL` | | LLM 端点（默认 `https://api.kimi.com/coding/v1`） |
| `KIMI_MODEL_NAME` | | 模型名（默认 `kimi-for-coding`） |
| `KIMI_DOC_INTEL_TIMEOUT` | | Round 0 文档情报超时秒数（默认 120） |
| `OSV_SCANNER_PATH` | | osv-scanner 二进制路径（留空自动探测 `./bin/osv-scanner` → PATH） |

## 运行

```bash
export KIMI_API_KEY=sk-...
huntzero scan https://github.com/owner/repo     # 或 python huntzero.py scan ...
huntzero scan /path/to/local/project -w 8 -R 50
```

常用参数（`huntzero scan --help`）：

- `--workers/-w`：并发 drone 数（默认 5）
- `--work-dir/-d`：工作目录（默认 `./local_workspace`）
- `--resume/-r`：断点续跑
- `--max-rounds/-R` / `--max-tasks/-T` / `--max-time/-t`：终止预算（默认 30 轮 / 200 任务 / 7200 秒）
- `--stagnation/-s`：连续 N 轮无新发现即停（默认 3）
- `--no-auto-approve`：关闭全自动（默认开启）
- `--output/-o` / `--no-report`：报告导出控制

## 测试体系

- 基线 **100 passed / 0 failed**；`python -m pytest` 全量约 3 秒
- 单测按模块覆盖：blackboard 去重与核心、cerebrum 解析与终止守卫、sector 预算与启发式、llm_config、osv_bridge、exploit_analyzer、semantic_helper、backends、bayesian 学习等
- 假 LLM 集成测试：`tests/fake_llm.py` + `tests/test_integration_cerebrum.py` 端到端复现「假设→任务→finding」全链路
- 表征/回归约定：审计中发现的疑似 bug 先登记到 `docs/suspicious-registry.md`，
  修复后翻转为正式回归测试（登记册含各项状态与 commit 对应）
