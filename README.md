# 🧠 kimiSec — HIVE-MIND INTEL ENGINE

[![Python](https://img.shields.io/badge/python-3.11+-yellow.svg)](https://www.python.org/)
[![Mode](https://img.shields.io/badge/Mode-LLM_Dominant-purple.svg)](#)
[![Interface](https://img.shields.io/badge/Interface-Headless_CLI-blue.svg)](#)
[![Distributed](https://img.shields.io/badge/Architecture-K8s_Cluster_Ready-green.svg)](#)

> **核心哲学：人从"驾驶员"降级为"授权官"。**
> LLM 以第一性原理自主推演，系统全程无需人工干预。

---

## ✨ 系统概述

kimiSec 是一个**以 LLM 为绝对主导**的分布式安全分析引擎，配套**企业级官网**与**情报同步管线**。

| 维度 | 设计选择 |
|------|---------|
| 分析模式 | 第一性原理 · 假设驱动 · 无检查清单 |
| 并发模型 | 异步 `asyncio` · Cerebrum 主脑 + 并发 Drone 工蜂 |
| 大型项目 | **Sector-Based Analysis** · 自动分区 · 并行 Mini-Cerebrum |
| 接口形态 | 纯 CLI 无 TUI · 集群版 MCP 接口 |
| 部署架构 | 单机 `kimi.py scan` / 容器化 `kimi.py worker` Redis 集群 |
| 人类角色 | 仅在不可逆边界操作时介入（单机版） / 战略排兵布阵（集群版） |

---

## 🏗️ 架构

```
python kimi_hive.py <target>
         │
         ▼
┌─────────────────────────────────────────────────────┐
│  kimi_hive.py  (CLI 入口 · 遥测渲染 · 仲裁 stdin)   │
└──────────────────────┬──────────────────────────────┘
                       │ asyncio
┌──────────────────────▼──────────────────────────────┐
│               CEREBRUM  (主脑编排器)                  │
│                                                     │
│  Round N: Observe → Hypothesize → Dispatch → Critique│
│  ┌─ _cerebrum_loop   ← LLM 推理 / JSON 解析 / 迭代  │
│  ├─ _drone_dispatcher ← 消费 task_queue · 创建 Task │
│  ├─ _result_integrator← 消费 result_queue · 整合结果│
│  └─ _sweep_orphaned   ← 假设→Finding 双层收割       │
│                                                     │
│  ┌─ Sector Manager ─────────────────────────────┐   │
│  │  大型项目自动分区 → 并行 Mini-Cerebrum 分析    │   │
│  │  · 动态预算 (priority × file_count)           │   │
│  │  · 输出隔离 (防止 JSON 交错)                   │   │
│  │  · 跨模块利用链合成                            │   │
│  └──────────────────────────────────────────────┘   │
└───────┬───────────────────────────────┬─────────────┘
        │ read/write                    │ 放入任务
┌───────▼──────────────┐    ┌──────────▼──────────────┐
│   BLACKBOARD (黑板)   │    │   DRONE WORKER POOL      │
│ · hypotheses (假设树) │    │   (最多 N 个并发)         │
│ · tasks (任务队列)    │    │   · 完全隔离的 Session    │
│ · findings (发现)     │    │   · 读 skills/           │
│ · 五层去重管线        │    │   · 多轮追踪 (Chase)      │
│ · arbitration_queue  │    │   · 返回结构化文本输出     │
│ · 持久化 .blackboard  │    │   · 用完即销毁            │
│   .json              │    └──────────────────────────┘
└──────────────────────┘
        │
        ▼  sync_intel.py (HMAC-SHA256)
┌──────────────────────────────┐
│  外网官网 (website_server.py) │
│  · GET /api/intel → 前端渲染  │
│  · intelligence.html 动态加载 │
└──────────────────────────────┘
```

### 第一性原理分析公理

Cerebrum 只寻找三类系统异常，不使用任何漏洞清单：

| 异常类型 | 定义 |
|---------|------|
| **逻辑矛盾** | 系统声明属性 X 成立，但结构证据显示 ¬X |
| **状态异常** | 合法操作序列将系统引入未定义或意外状态 |
| **越界资源访问** | 主体能引用/读写/执行超出其声明范围的资源 |

---

## 🔬 核心引擎能力

### Sector-Based Analysis（大型项目分区分析）

对超过 500 文件的大型代码库（如 GhostPDL、Ghostscript），引擎自动启用 Sector 模式：

1. **SectorManager** 按目录结构和文件类型自动分区，为每个 Sector 评估优先级
2. 每个 Sector 运行独立的 **Mini-Cerebrum**，拥有隔离的 BlackboardPartition
3. **动态预算分配**：根据 Sector 优先级和文件数量自动调整 `max_tasks` 和 `max_rounds`
   - P0 (高危) Sector: 最多 180 任务 / 25 轮
   - P3 (低危) Sector: 最少 32 任务 / 8 轮
4. 所有 Sector 完成后进行 **跨模块利用链合成**（Cross-Sector Exploit Chain Synthesis）

### 假设去重管线（五层级）

防止 LLM 对同一漏洞点反复生成重复假设：

| 层级 | 策略 | 说明 |
|------|------|------|
| **Layer 0** | 锚点标识符 | 提取函数名 (`TIFFReadDirEntryArray`) 和文件名 (`ramfs.c`)，精准匹配 |
| **Layer 1** | 结构指纹 | 排序后的归一化 token 完全相同 |
| **Layer 2** | Bigram Jaccard | 短语级重合度 ≥ 0.55 |
| **Layer 3** | Unigram Jaccard | 词袋级重合度 ≥ 0.65 |
| **Layer 4** | 字符三元组 | 模糊匹配 ≥ 0.70（兜底层） |

### 假设晋升机制（双层收割）

确保高置信假设不会因为 Critic 流程中断而丢失：

- **Layer 1**: CONFIRMED/SUSPECTED 状态 + confidence ≥ 0.5 → 提升为 Finding
- **Layer 2**: 任何状态 + confidence ≥ 0.85 → 强制提升为 Finding（`[auto-promoted]`）

### 领域驱动管线（Domain Pipeline）

引擎根据项目特征自动检测适用领域，按阶段推进分析：

```
Attack Surface Mapping → Deep Dive → Exploit Validation → Report
```

每个阶段有独立的轮次预算和推进条件，防止过早结束或无限循环。

### Adversarial Critic（对抗性审查）

Drone 发现的每个候选漏洞都会经过独立的 Critic Session 审查：

- 扮演**防守方架构师**角色，反驳 Drone 的结论
- 输出三元组 `(decision, reasoning, severity)`
- 只有通过 Critic 审查的发现才直接进入报告；未通过的走 Layer 2 收割兜底

---

## 📁 目录结构

```
kimiSec/
│
├── kimi.py                   # 🚀 统一入口 (scan / serve / worker / feed)
├── kimi_hive.py              # 📦 CLI 分析入口 (kimi.py scan 委托调用)
├── sync_intel.py             #  情报同步工具 (内网→外网 HMAC 推送)
├── sync_wildfire.py          # 🔥 在野漏洞情报 (CISA KEV 自动监测)
├── .bots.md                  # 🧠 系统基础提示词 (第一性原理版)
│
├── engine/                   # ⚙️  Hive-Mind 引擎
│   ├── blackboard.py         #    全局黑板 (SSOT · 五层去重 · 异步安全 · 持久化)
│   ├── cerebrum.py           #    主脑编排器 (推理·调度·Sector分区·双层收割)
│   ├── drone.py              #    工蜂节点 (原子执行·多轮追踪·隔离Session)
│   ├── sector.py             #    Sector 分区管理器 (大型项目自动分区)
│   ├── fuzzer.py             #    模糊测试集成
│   ├── backends.py           #    可插拔存储后端 (Local / Redis)
│   └── semantic_helper.py    #    代码语义分析 (tree-sitter)
│
├── server/                   # 🌐 统一服务层
│   ├── core.py               #    KimiSecCore 控制器 (local/cluster 自动切换)
│   ├── mcp.py                #    MCP 协议 (stdio + TCP)
│   ├── http.py               #    FastAPI REST + WebSocket
│   └── worker.py             #    Redis BLPOP Worker
│
├── index/                    # 🌍 企业官网 (lieling.xyz)
│   ├── website_server.py     #    FastAPI 官网后端
│   ├── index.html            #    首页
│   ├── intelligence.html     #    漏洞情报 (动态加载 /api/intel)
│   ├── services.html         #    服务介绍
│   ├── about.html            #    关于我们
│   ├── contact.html          #    联系我们
│   ├── css/style.css         #    全局设计系统
│   ├── js/main.js            #    前端交互
│   ├── data/                 #    数据存储 (intel.json, leads.json)
│   └── nginx/prezero.conf    #    生产 Nginx 配置
│
├── skills/                   # 📚 领域情报简报 (战术知识库)
├── knownAttack/              # ⚔️  已知攻击自动化框架
├── model_q/                  # 🎓 模型训练工具
├── patchdiff/                # 🔍 补丁差异分析
├── tools/                    # 🔧 辅助工具
│
├── Dockerfile                # 🐳 Worker 容器镜像
├── Dockerfile.fuzzer         # 🐳 Fuzzer 容器镜像
├── docker-compose.yml        # 🐳 集群编排 (Redis + Worker×3 + Server)
├── pyproject.toml            # 📦 项目元数据 (pytest / black / ruff)
├── .env.example              # 🔑 环境变量模板
└── local_workspace/          # 💾 分析产物 (运行时自动创建)
```

---

## 🚀 快速开始

### 1. 安装依赖

```bash
pip install -r requirements.txt

# 开发模式（含 pytest / black / ruff / mypy）
pip install -e ".[dev]"
```

### 2. 配置环境变量

```bash
cp .env.example .env
# 编辑 .env 填入真实的 API Key 和密钥
```

| 变量 | 必需 | 说明 |
|------|------|------|
| `KIMI_API_KEY` | ✅ | Moonshot AI API Key |
| `KIMI_BASE_URL` | — | 自定义 API 端点（默认 `https://api.kimi.com/coding/v1`） |
| `INTEL_SECRET` | ⚠️ | 情报同步 HMAC 密钥（生产环境必须修改） |
| `WECOM_WEBHOOK` | — | 企业微信通知 Webhook URL |

### 3. 运行扫描

```bash
# 分析 GitHub 仓库
python kimi.py scan https://github.com/owner/repo

# 或直接使用 kimi_hive.py（更多参数控制）
python kimi_hive.py /path/to/project --workers 8 --resume

# 参数说明
python kimi_hive.py <target> \
    --workers 8         \   # 最大并发 Drone 数
    --max-rounds 30     \   # 最大推理轮次
    --max-tasks 200     \   # 最大任务数
    --max-time 7200     \   # 最大运行时间(秒)
    --stagnation 3      \   # 停滞检测轮次
    --auto-approve      \   # 无人值守模式
    --resume            \   # 断点续传
    --output report.md      # 指定输出路径
```

### 4. 查看结果

- **终端**：彩色结构化报告直接打印（Severity 分级着色）
- **`reports/<target>_<timestamp>.md`**：Markdown 格式完整报告
- **`reports/<target>_<timestamp>.json`**：JSON 机器可读报告
- **`.blackboard.json`**：完整假设树 + 任务记录 + 发现

---

## 🛡️ 稳定性保障

引擎内置多层稳定性机制，确保长时间大规模扫描的可靠性：

| 问题 | 保护机制 |
|------|---------|
| SDK 步数爆炸 (MaxStepsReached) | Drone + Cerebrum 双层捕获，保留部分结果 |
| 并行输出交错 | Sector 模式禁用逐 chunk 流式输出，改为轮摘要 |
| Event loop 关闭异常刷屏 | 自定义异常处理器 + GC 前置 + stderr 过滤器（三层防御） |
| 假设丢失 | 双层收割机制（Layer 1 + Layer 2 auto-promoted） |
| 预算过早耗尽 | 动态预算分配（priority × file_count） |
| 假设重复膨胀 | 五层去重管线（锚点 → 指纹 → Bigram → Unigram → Trigram） |
| Drone 超时 | 自动重试 + 部分结果保留 + 超时注解 |
| 仲裁阻塞 | 支持 `--auto-approve` 无人值守模式 |

---

## 🌐 官网部署 (lieling.xyz)

### 本地开发

```bash
cd index
python website_server.py
# → http://localhost:8000
```

### 生产部署

1. **启动 FastAPI 后端**（推荐用 systemd 管理）：
```bash
cd /opt/kimiSec/index
INTEL_SECRET="强随机密钥" WECOM_WEBHOOK="https://..." python website_server.py
```

2. **配置 Nginx**：
```bash
cp index/nginx/prezero.conf /etc/nginx/conf.d/
# 修改 SSL 证书路径和项目目录路径
nginx -t && systemctl reload nginx
```

3. **申请 SSL 证书**：
```bash
certbot certonly --webroot -w /var/www/certbot -d lieling.xyz -d www.lieling.xyz
```

---

##  情报同步 (内网→外网)

挖掘到的漏洞通过 `sync_intel.py` 从内网安全推送到官网：

```
内网 kimi.py 审计 → blackboard.json → sync_intel.py
                                           │ HTTPS POST (HMAC-SHA256)
                                           ▼
                                     外网 /api/intel → intel.json → intelligence.html
```

### 推送单条

```bash
python sync_intel.py push \
    --url https://lieling.xyz/api/intel \
    --secret "你的HMAC密钥" \
    --component "fastjson" --version "1.2.83" \
    --title "autoType RCE" --severity critical --cvss 9.8 \
    --visibility locked
```

### 从 blackboard 批量同步

```bash
python sync_intel.py sync \
    --url https://lieling.xyz/api/intel \
    --secret "你的HMAC密钥" \
    --blackboard ./local_workspace/.blackboard.json
```

### 查看已同步情报

```bash
python sync_intel.py list --url https://lieling.xyz/api/intel
```

> **可见性规则**: `critical/high` → locked（订阅专享），`medium/low` → public（公开）

---

## 🌐 分布式集群部署

### Docker Compose 一键启动

```bash
# 创建环境配置
cp .env.example .env
# 编辑 .env 填入 KIMI_API_KEY

# 启动集群 (1 Redis + 3 Worker + 1 Server)
docker compose up -d

# 动态扩容到 50 个 Worker
docker compose up -d --scale worker=50
```

### 投递扫描任务

```bash
# 方式 1: 直接压入 Redis
redis-cli RPUSH kimisec:targets '{"target":"https://github.com/apache/kafka"}'

# 方式 2: 通过 MCP 接口（配合 OpenClaw）
# 连接到 localhost:9999 的 MCP 服务器
```

---

## 🧪 测试

```bash
# 运行完整测试套件
python -m pytest

# 运行单个测试文件
python -m pytest tests/test_example.py

# 代码格式化与 lint
black --line-length 100 .
ruff check .
```

---

## 🎮 操作方式

| 场景 | 操作 |
|------|------|
| 启动分析 | 直接运行命令，Hive 自动循环 |
| 仅有的人类交互 | 仲裁弹出时，按 `Y` 授权或 `N` 拒绝 |
| 强制中断 | `Ctrl+C`（优雅退出，保留 Blackboard 状态） |
| 无人值守 | `python kimi_hive.py <target> --auto-approve` |
| 断点续传 | `python kimi_hive.py <target> --resume` |

---

## License

MIT © kimiSec Hive-Mind Team
