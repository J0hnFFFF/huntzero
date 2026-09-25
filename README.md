<div align="center">

# huntzero

**以第一性原理推演漏洞的 LLM 分析引擎**

从假设出发，而不是从清单出发。

![Python](https://img.shields.io/badge/Python-3.11%2B-blue?style=flat-square)
![Interface](https://img.shields.io/badge/Interface-CLI-black?style=flat-square)
![Tests](https://img.shields.io/badge/Tests-100%20passed-brightgreen?style=flat-square)
![Delivery](https://img.shields.io/badge/Delivery-Cython%20%7C%20VM-orange?style=flat-square)
![License](https://img.shields.io/badge/License-Proprietary-lightgrey?style=flat-square)

</div>

---

## 为什么不一样

传统扫描器把已知漏洞模式编成清单——列表之外的一律失明。
huntzero 换了一条路：**不告诉它要找什么，只告诉它什么算异常**。

| | 传统规则扫描 | huntzero |
|---|---|---|
| **驱动方式** | 模式匹配清单 | 假设驱动的自主推演 |
| **知识来源** | CVE / 规则库 | 第一性原理 + 领域地形情报 |
| **验证方式** | 静态判定 | 对抗性审查 + 真实执行闭环 |
| **产出** | 告警列表 | 带证据链的可信发现 |

引擎只寻找三类系统异常：

> **逻辑矛盾** — 系统声称性质 X，代码却呈现 ¬X
> **状态异常** — 合法操作序列抵达未定义 / 非预期状态
> **越界访问** — 主体可引用声明作用域之外的资源

---

## 快速开始

```bash
# 安装（Python ≥ 3.11）
pip install -e ".[dev]"

# 配置 LLM 认证
export KIMI_API_KEY=sk-...

# 扫描
huntzero scan https://github.com/owner/repo
huntzero scan /path/to/local/project --workers 8
```

扫描结束后终端打印分级摘要，报告自动落盘到 `./reports/<target>_<timestamp>.md`。

---

## 工作方式

<div align="center">

```
                 ┌──────────────────────────────────────────┐
                 │            CEREBRUM  主脑                │
                 │   提假设 ── 派任务 ── 审结果 ── 判终止    │
                 │            （无工具，纯推理）            │
                 └──────┬────────────────────────┬──────────┘
                        │ 读/写                  │ 派发微任务
                        ▼                        ▼
             ┌────────────────────┐   ┌──────────────────────┐
             │     BLACKBOARD     │   │    DRONE 工蜂池       │
             │   唯一共享状态中心  │◄──│  沙盒取证 · 多轮追踪  │
             │   假设树 · 任务 · 发现│   │  用完即焚            │
             └────────────────────┘   └──────────────────────┘
```

</div>

一次扫描的完整链路：

```
① 目标准备       git clone（URL）或直接使用本地路径
② 依赖前置       OSV-Scanner 扫依赖漏洞，注入黑板
③ 侦察与情报     收窄范围 + 提取项目文档上下文
④ 领域路由       按暴露面加载领域情报，构建九阶段分析管线
⑤ 主循环         假设 → 任务 → 工蜂取证 → 对抗审查 → 发现
⑥ 深化闭环       PoC 验证 / 证伪 drone；harness 在断网容器真实 fuzz
⑦ 自动分区       大型目标切 Sector 并行，最后合成跨模块利用链
⑧ 终止与报告     五层终止守卫；导出 Markdown / JSON / 利用链图
```

> 细节见 **[docs/architecture.md](docs/architecture.md)**

---

## 报告示例

````markdown
# Security Analysis Report

| Field     | Value                    |
|-----------|--------------------------|
| Engine    | HUNTZERO INTEL ENGINE V8.0 |
| Target    | `ghostpdl`               |
| Duration  | 2841s                    |

## Summary

| Metric           | Count |
|------------------|-------|
| Hypotheses       | 87    |
| Confirmed        | 12    |
| Zero-Day Findings| 3     |
| Tasks Executed   | 164   |

## Zero-Day Findings

### 🔴 [1] Integer truncation in ramfile_seek enables out-of-bounds read

- **Severity**: CRITICAL
- **Hypothesis**: H-3F2A1C
- **ID**: F-91B04E

**Description:**
`ramfile_seek` casts a 64-bit `gs_offset_t` to `int` before assignment...

**Evidence:**
```
base/ramfs.c:142  →  handle->filepos = (int)newpos;
调用链：s_ram_write_process → ramfile_write → 截断后越界
```
````

报告同时导出：

| 产物 | 说明 |
|---|---|
| `<name>.md` | 人类可读：分级发现、证据链、复现步骤 |
| `<name>.json` | 机器可读：完整结构化数据 |
| `<name>.dot` / `.graphml` | 利用链图（`dot -Tsvg` 渲染） |
| `.blackboard.json` | 假设树 + 任务 + 全部发现的完整状态（断点续跑依据） |
| `.audit_notes.md` | 引擎增量写入的分析轨迹 |

---

## 命令行

```
huntzero scan <target> [选项]
```

| 选项 | 默认 | 说明 |
|---|---|---|
| `-w, --workers N` | 5 | 并发工蜂数（提速 ⇆ 瞬时成本） |
| `-R, --max-rounds N` | 30 | 推理轮次上限（分析的深度） |
| `-T, --max-tasks N` | 200 | 任务数上限（成本上界） |
| `-t, --max-time S` | 7200 | 墙钟时间上限（秒） |
| `-s, --stagnation N` | 3 | 连续 N 轮无新发现即停 |
| `-d, --work-dir PATH` | `./local_workspace` | 工作目录 |
| `-r, --resume` | — | 断点续跑（从黑板恢复） |
| `-o, --output PATH` | 自动 | 报告路径（`.md` / `.json`） |
| `--[no-]auto-approve` | 开启 | 无人值守仲裁 |
| `--no-report` | — | 不落盘报告 |

常用配方与参数取舍详见 **[docs/usage.md](docs/usage.md)**。

---

## 配置

`.env.example` 为模板；新增变量由 `tests/test_env_sync.py` 强制同步。

| 变量 | 必需 | 说明 |
|---|---|---|
| `KIMI_API_KEY` | ✅ | LLM 认证 |
| `KIMI_BASE_URL` | | LLM 端点（默认 `https://api.kimi.com/coding/v1`） |
| `KIMI_MODEL_NAME` | | 模型名（默认 `kimi-for-coding`） |
| `KIMI_DOC_INTEL_TIMEOUT` | | 文档情报阶段超时，秒（默认 120） |
| `OSV_SCANNER_PATH` | | osv-scanner 路径（默认自动探测） |

---

## 架构一览

| 组件 | 职责 | 关键机制 |
|---|---|---|
| **Cerebrum** 主脑 | 推理与裁决 | 无工具纯推理 · JSON 强制输出 · 领域管线 · 五层终止守卫 |
| **Drone** 工蜂 | 沙盒取证 | 任务原子化 · 目录隔离 · 多轮追踪 · 会话池预热 |
| **Blackboard** 黑板 | 唯一事实源 | 五层语义去重 · 贝叶斯置信度 · 原子落盘 |
| **Sector** 分区 | 大型目标 | 自动分区 · 隔离预算 · 跨模块利用链合成 |
| **Critic** 审查 | 对抗验证 | 防守方视角驳回 · 异常放行不丢发现 |
| **Fuzzer** 闭环 | 真实验证 | 断网容器 · libFuzzer · crash 即 critical |

<details>
<summary><b>完整目录结构</b></summary>

```
huntzero.py        CLI 入口（唯一子命令 scan）
kimi_hive.py       扫描主流程编排
kimi_sdk_compat.py SDK 兼容补丁
engine/            引擎核心
  cerebrum.py        主脑：推理循环 / 领域管线 / 终止守卫 / CRITIC
  drone.py           工蜂：沙盒取证、多轮追踪
  blackboard.py      黑板：去重 / 置信度 / 仲裁 / 持久化
  sector.py          大型目标自动分区
  backends.py        存储后端（原子写）
  llm_config.py      LLM 配置构建
  osv_bridge.py      依赖漏洞桥
  semantic_helper.py AST 语义导航
  session_pool.py    会话池
  fuzzer.py          模糊测试沙盒
skills/            14 个领域情报库（web/binary/browser/mail/supply-chain/ai-agent…）
tools/             check_env_sync · build_cython · poc_sandbox · exploit_analyzer · finops_monitor
tests/             测试套件（含假 LLM 端到端 harness）
docs/              架构 / 使用指南 / 可疑点登记册 / 设计存档
```

</details>

---

## 工程与质量

```bash
python -m pytest                 # 100 passed / 0 failed（约 3 秒）
python tools/check_env_sync.py   # 环境变量一致性门禁
bash tools/build_cython.sh       # Cython 编译管线（源码版/编译版测试双跑比对）
```

- **测试锁行为**——去重管线、置信度传播、解析规则、终止判定、预算分配均有单测；
  `fake_llm.py` 以脚本化 LLM 端到端驱动主循环，任何编排回归即时暴露
- **可疑点登记册**——审计发现的疑似问题一律先记录、后验证、再动手；
  确认的缺陷修复后翻转为回归测试（轨迹见 `docs/suspicious-registry.md`）
- **编译交付**——引擎与工具编译为原生 `.so`，同一套测试对源码与编译产物双跑一致

---

## 文档

| | |
|---|---|
| [docs/usage.md](docs/usage.md) | 使用指南：参数详解 · 场景配方 · 报告解读 · 故障排查 |
| [docs/architecture.md](docs/architecture.md) | 架构详解：组件 · 扫描生命周期 · 去重与置信度 · 终止判定 |
| [docs/suspicious-registry.md](docs/suspicious-registry.md) | 可疑点登记册（质量审计轨迹） |
| [docs/cython-spike.md](docs/cython-spike.md) | Cython 编译管线与双跑验证结论 |

---

<div align="center">

**huntzero** · Proprietary · © huntzero Team

*Find what the lists don't know.*

</div>
