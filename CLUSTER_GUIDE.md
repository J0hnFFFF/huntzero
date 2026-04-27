# kimiSec 工业级分布式集群使用指南

本指南将带你了解如何将 kimiSec 从单机工具扩展为支撑千万级吞吐量的大规模分布式漏洞挖掘兵工厂。

---

## 🏗️ 架构概览

kimiSec 集群架构由三个核心组件构成：

1.  **Redis (消息总线 & 情报中心)**
    *   作为集群的大脑，它不执行分析，只负责存储：
        *   `kimisec:targets` (List) - 等待分析的项目队列（GitHub 链接、GitLab 链接或文件路径）。
        *   `kimisec:node:{id}:snapshot` (Hash) - 正在运行的节点的实时推理状态树。
        *   `kimisec:global:findings` (Sorted Set) - 所有节点挖掘出的高危漏洞排行榜。
        *   `kimisec:global:events` (Pub/Sub) - 集群内的实时遥测事件（如 "找到了一个 0day!"）。

2.  **Worker 节点 (`kimi.py worker`)**
    *   可以水平无限扩展的 "执行干员"。你可以起 1 个，也可以起 10,000 个。
    *   它们默认处于阻塞监听状态。一旦 Redis 队列里进来了新的项目 URL，空闲的 Worker 就会抢占它，拉取代码，在本地沙箱里运行数十个 Drone 进行深度的第一性原理推演。
    *   推演完成（或因超时/资源耗尽而终止）后，它会将最终的分析结果持久化到 Redis，然后继续监听下一个任务。

3.  **统一服务 (`kimi.py serve`)**
    *   同时提供 MCP 协议接口和 HTTP Web 控制台。
    *   MCP 接口供外部上级指挥官（如 OpenClaw 宏观决策 Agent）使用：一键投递 1000 个项目、实时查看节点状态、跨项目搜索漏洞模式。
    *   HTTP 接口提供浏览器可视化仪表盘、REST API 和 WebSocket 实时事件流。

---

## 🚀 一键式 Docker 部署

得益于无状态设计，部署极其简单。

### 1. 配置文件
在项目根目录创建一个 `.env` 文件，填入模型 API Key：
```bash
KIMI_API_KEY=sk-xxxx...xxxx
```

### 2. 通过 Docker Compose 拉起集群
项目根目录已经准备好了 `docker-compose.yml`，默认包含 1 个 Redis + 3 个 Worker + 1 个统一服务：

```bash
docker compose up -d
```

### 3. 如何横向扩容？
如果你拿到了大量云计算资源（或者发现队列里积压了上千个任务），只需一行命令即可动态扩展工蜂大军（例如扩充到 50 个节点）：

```bash
docker compose up -d --scale worker=50
```

### 4. 访问控制台
- **Web 仪表盘**: http://localhost:8080
- **MCP TCP 端口**: localhost:9999

---

## 🎮 控制台操作 / 手动投递任务

### 方式 1: 直接操控 Redis 队列
最暴力的手段，直接把要审查的 URL 踢进目标序列：

```bash
docker exec -it kimisec-redis redis-cli

# 投递一个大项目
RPUSH kimisec:targets '{"target":"https://github.com/nginx/nginx", "max_rounds": 100, "max_workers": 10}'

# 投递一个小工具
RPUSH kimisec:targets '{"target":"https://github.com/author/small-tool"}'
```
（注：JSON 中可以覆写每个项目特有的并发数 `max_workers` 和 LLM 推理深度 `max_rounds`）

### 方式 2: 通过 Web API 投递
```bash
curl -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{"targets": [{"target": "https://github.com/owner/repo"}]}'
```

### 方式 3: 查看集群健康度与 Findings 排行榜
```bash
# 集群状态
curl http://localhost:8080/api/cluster

# 所有漏洞发现
curl http://localhost:8080/api/findings

# 跨项目模式分析
curl http://localhost:8080/api/insights
```

---

## 🌟 跨项目归因 (Cross-Project Analysis) 

这是 kimiSec 集群版的最致命的杀招：**供应链污染溯源**。

假设某日：
- Node-13 在扫描 `Project-A` 时，发现了一个由于 `Lodash` 某老版本导致的 RCE 定论。
- Node-87 在扫描 `Project-X` 时，也定论了一个同样的内存泄漏路径。

OpenClaw 作为上级视角，会定期扫视 `kimisec:global:findings`。当它看到这两个看似毫无关联的漏洞时，它会基于它长文本大模型的优势总结出：
> *"报告长官：这不仅仅是两个漏洞。我们发现了跨越 18 个微服务/开源仓库的同一型号 0-day 爆发。这似乎是底层的日志组件被投毒所致。"*

---

## 常见问题 (FAQ)

**Q: 为什么不拆分目标？比如把 Linux 内核拆成 50 份交给 50 个 Node 算？**
A: **千万不要这么做**。漏洞通常隐藏在状态的不一致中（例如子模块 A 向模块 B 报告成功，但实际上结构变了，导致 B 指针崩溃）。如果你把项目物理切开给不同的机器读，这 50 台机器谁也看不到全局矛盾，就退化成了愚蠢的正则扫描器。**记住：集群算力的横向扩展只能用来增加能够并行的目标数量（例如同时扫 50 个不同的程序）；对于单一的超大目标，你应该加大单节点的内存和 CPU，加上 `--workers 200`（这叫纵向扩展）去强攻。**

**Q: 我需要在宿主机上装代码环境吗？**
A: 不需要。`docker-compose` 起的 Worker 镜像内已经打包了 Python、Tree-sitter、Git 等基础设施，并完全隔离在容器内防止恶意沙箱逃逸。

**Q: Worker 卡死了/断网了怎么办？**
A: 没有关系。如果节点 2 分钟不发送心跳（心跳写入 Redis Hash），在集群监控视角就会被标记为死亡（`alive: false`）。该节点负责的目标可以安排人工或其他手段重启，Redis 会自动基于 7 天 TTL 回收僵尸节点的垃圾快照，绝不内存泄漏。
