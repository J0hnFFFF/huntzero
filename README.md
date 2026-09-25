# huntzero

**LLM 主导的假设驱动漏洞挖掘引擎 —— 纯 CLI 交付。**

huntzero 不使用预定义漏洞清单，而是从第一性原理出发，围绕三类系统异常做假设驱动的自主推理：

- **逻辑矛盾（Logical Contradictions）**——系统声称性质 X，代码却呈现 ¬X
- **状态异常（State Anomalies）**——合法操作序列抵达未定义/非预期状态
- **越界访问（Unbound Resource Access）**——主体可引用声明作用域之外的资源

主脑（Cerebrum）只做假设与裁决，工蜂（Drone）在隔离沙盒中取证，发现经对抗性审查（Critic）后沉淀为黑板上的 Finding，最终产出结构化报告。

> License: **Proprietary**（见 `pyproject.toml`）。本仓库为私有产品源码。

---

## 快速开始

```bash
# 1. 安装
pip install -e ".[dev]"

# 2. 配置 LLM 认证
export KIMI_API_KEY=sk-...

# 3. 扫描（GitHub 仓库或本地路径）
huntzero scan https://github.com/owner/repo
huntzero scan /path/to/local/project --workers 8
```

扫描结束后终端打印分级报告，并自动导出到 `./reports/<target>_<timestamp>.md`。
完整参数说明见 **[docs/usage.md](docs/usage.md)**。

---

## 工作原理

```
                    ┌─────────────── CEREBRUM（主脑，无工具，输出强制 JSON）
                    │  第 N 轮：观察 → 提假设 → 派任务 → 审结果
                    ▼
  observe ──► hypotheses ──► tasks ──► [DRONE 池] ──► results ──► critic ──► findings
     ▲                                  │ 沙盒隔离        │
     └────────── BLACKBOARD（唯一共享状态 / SSOT）◄────────┘
```

一次扫描的完整链路：

1. **目标准备**——URL 则 `git clone --depth 1` 到工作目录；本地路径直接使用
2. **依赖前置**——OSV-Scanner 扫依赖漏洞注入黑板（可选，缺失自动跳过）
3. **战略侦察 + 文档情报**——收窄分析范围，提取项目上下文
4. **领域路由**——按暴露面（而非语言）从 `skills/` 加载领域情报，构建九阶段分析管线
5. **主循环**——每轮 LLM 产出假设与微任务；Drone 在隔离沙盒中执行并回报；经 Critic（防守方视角对抗审查）后成为 Finding
6. **深化闭环**——critical/high 发现自动追加 PoC 验证与证伪 drone；`harness-generator` 产出的模糊测试任务在断网容器中真实运行
7. **大型目标自动分区**——超过阈值自动切成 Sector 并行分析，最后做跨模块利用链合成
8. **终止与报告**——五层终止守卫；导出 Markdown / JSON / 利用链图（DOT、GraphML）

架构细节见 **[docs/architecture.md](docs/architecture.md)**。

---

## 输出

| 产物 | 位置 | 说明 |
|---|---|---|
| Markdown 报告 | `reports/<target>_<ts>.md` | 分级发现、证据链、复现步骤 |
| JSON 报告 | `--output x.json` | 机器可读，含完整结构化数据 |
| 利用链图 | 同报告目录 `.dot` / `.graphml` | 跨模块利用链可视化 |
| 全套状态 | `<work-dir>/.blackboard.json` | 假设树 + 任务 + 发现（断点续跑依据） |
| 审计笔记 | `<work-dir>/.audit_notes.md` | 引擎增量写入的分析轨迹 |

## 命令行

```
huntzero scan <target> [选项]

  -w, --workers N      并发 drone 数（默认 5）
  -d, --work-dir PATH  工作目录（默认 ./local_workspace）
  -r, --resume         从上次中断处续跑
  -R, --max-rounds N   推理轮次上限（默认 30）
  -T, --max-tasks N    任务数上限（默认 200）
  -t, --max-time S     墙钟时间上限秒（默认 7200）
  -s, --stagnation N   连续 N 轮无新发现即停（默认 3）
      --[no-]auto-approve  无人值守仲裁（默认开启）
  -o, --output PATH    报告路径（.md 或 .json；默认自动命名）
      --no-report      不导出报告文件
```

## 配置

`.env.example` 为模板；新增变量必须同步（`tests/test_env_sync.py` 强制检查）。

| 变量 | 必需 | 说明 |
|---|---|---|
| `KIMI_API_KEY` | ✅ | LLM 认证 |
| `KIMI_BASE_URL` | | LLM 端点（默认 `https://api.kimi.com/coding/v1`） |
| `KIMI_MODEL_NAME` | | 模型名（默认 `kimi-for-coding`） |
| `KIMI_DOC_INTEL_TIMEOUT` | | 文档情报阶段超时秒数（默认 120） |
| `OSV_SCANNER_PATH` | | osv-scanner 路径（默认自动探测 `./bin/osv-scanner` → PATH） |

## 目录结构

```
huntzero.py        CLI 入口（子命令只有 scan）
kimi_hive.py       扫描主流程编排（会话、报告导出）
kimi_sdk_compat.py kimi-agent-sdk 兼容补丁
engine/            引擎核心
  cerebrum.py        主脑：推理循环 / 领域管线 / 终止守卫 / CRITIC
  drone.py           工蜂：沙盒取证、多轮 chase
  blackboard.py      黑板 SSOT：五层去重 / 贝叶斯置信度 / 仲裁
  sector.py          大型目标自动分区
  backends.py        存储后端（Local 原子写）
  llm_config.py      LLM 配置构建
  osv_bridge.py      依赖漏洞桥
  semantic_helper.py AST 语义导航（drone 的精确验证通道）
  session_pool.py    会话池（降低 drone 冷启动开销）
  fuzzer.py          模糊测试沙盒
skills/            14 个安全领域情报库（按暴露面加载）
tools/             check_env_sync / build_cython / poc_sandbox / exploit_analyzer / finops_monitor / install_osv
tests/             测试套件（含假 LLM 集成 harness 与 fixture 项目）
docs/              架构、使用指南、登记册、设计存档
```

## 测试与质量

```bash
python -m pytest                 # 全量：100 passed / 0 failed，约 3 秒
python tools/check_env_sync.py   # 环境变量一致性门禁
bash tools/build_cython.sh       # Cython 编译管线（双跑验证见 docs/cython-spike.md）
```

- 单测覆盖黑板去重与置信度、cerebrum 解析与终止守卫、扇区预算、LLM 配置、OSV 桥、exploit 分析、语义工具等
- `tests/fake_llm.py` + `test_integration_cerebrum.py`：脚本化假 LLM 端到端复现「假设→任务→finding」全链路
- **可疑点纪律**：审计发现的疑似问题先登记 `docs/suspicious-registry.md`（含根因、证据、状态），
  确认是 bug 后修复并翻转为正式回归测试——不在无证据时改动引擎行为

## 成本与预算

扫描消耗全部来自 LLM 调用，由三重预算上限约束（`--max-rounds` / `--max-tasks` / `--max-time`）。
大型项目自动进入 Sector 模式时按扇区优先级动态分配预算。
`tools/finops_monitor.py` 提供 token 用量与成本的扫描后统计。

## 文档

| 文档 | 内容 |
|---|---|
| [docs/usage.md](docs/usage.md) | 使用指南：参数详解、场景配方、报告解读、断点续跑、故障排查 |
| [docs/architecture.md](docs/architecture.md) | 架构详解：组件、扫描生命周期、去重与置信度、终止判定 |
| [docs/suspicious-registry.md](docs/suspicious-registry.md) | 可疑点登记册（质量审计轨迹） |
| [docs/cython-spike.md](docs/cython-spike.md) | Cython 编译管线与双跑验证结论 |
| [docs/design/](docs/design/) | 武器化流程设计存档 |

---

© huntzero Team · Proprietary
