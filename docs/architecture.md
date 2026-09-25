# huntzero 架构详解

> 面向引擎维护者。描述"系统内部如何运转"以及为什么这样设计。
> 使用层面的文档见 [usage.md](usage.md)。

## 1. 总体结构

huntzero 是一个**黑板架构（Blackboard Architecture）**的多智能体系统：

```
                         ┌────────────────────────────────────┐
                         │            CEREBRUM                │
                         │  战略推理 · 假设/任务生成 · 裁决    │
                         │  领域管线 · 终止守卫 · CRITIC       │
                         └───────┬───────────────────┬────────┘
                    写假设/任务 │                   │ 派发微任务
                                 ▼                   ▼
                    ┌────────────────────┐  ┌──────────────────────┐
                    │    BLACKBOARD      │  │    DRONE 池（N 并发） │
                    │  唯一共享状态 SSOT  │◄─│  沙盒隔离 · 多轮 chase│
                    │  假设树/任务/发现   │  │  用完即焚            │
                    └────────────────────┘  └──────────────────────┘
```

**核心原则**：主脑与工蜂之间没有任何直接调用，一切经黑板中转。主脑被物理剥夺执行工具（`Session` 的 `tools` 为空），只能推理并输出结构化 JSON；工蜂不掌握全局目标，只拿到一条极度具体的微任务。

职责边界：

| 组件 | 文件 | 不做的事 |
|---|---|---|
| Cerebrum 主脑 | `engine/cerebrum.py` | 不执行代码、不读文件（无工具） |
| Drone 工蜂 | `engine/drone.py` | 不做全局判断、不修改其他任务状态 |
| Blackboard 黑板 | `engine/blackboard.py` | 不主动运行任何逻辑 |
| Sector 分区 | `engine/sector.py` | 不参与单文件级推理 |

## 2. Cerebrum（主脑）

### 2.1 推理循环

每轮循环做四件事，产出被强制约束为 JSON（SDK 层注入 `response_format=json_object`）：

1. **观察**——汇总黑板状态（活跃假设、未决任务、近期发现）与阶段目标，构造轮次 prompt
2. **提假设**——`hypotheses[]`（claim + confidence），经黑板入口的领域不变性校验后入树
3. **派任务**——`tasks[]`（hypothesis_ref + role + description），解析引用后入任务队列
4. **审结果**——消费工蜂回报，更新假设置信度，决定阶段推进或终止

`_parse_json_output` 的硬规则：假设置信度 `≤0.15` 丢弃、否定性表述丢弃；Finding 需 `confidence ≥ 0.70`；超过 15 个活跃假设时按置信度保留 top-15（GC）。

### 2.2 领域管线（Domain Pipeline）

引擎不使用漏洞清单，但使用**领域地形情报**：启动时经 `skills/security-expert/SKILL.md` 路由表，按目标暴露面（Web / 二进制 / 浏览器 / 邮件 / 供应链 / AI Agent 等 14 个领域）加载对应领域简报，并把阶段推进映射为九阶段管线：

```
target-definer → code-understander → vuln-hunter → hypothesis-tester →
variant-analyzer → validator → exploit-builder → poc-generator → report-generator
```

领域简报是"地形知识"（哪些结构天然易碎、哪些不变量容易失效），不是"检查项清单"——保持第一性原理推断的纯度。

### 2.3 CRITIC（对抗性审查）

每个工蜂报告的候选发现都要过独立的 Critic Session：扮演**防守方架构师**，逐条反驳"这为什么不是漏洞"，输出 `(decision, reasoning, severity)`。

工程约定：Critic 任何异常/超时**默认 ACCEPT**——宁可放行进入后续验证，也不因审查环节故障丢发现。未通过 Critic 的高置信假设走兜底收割（`[auto-promoted]`）。

### 2.4 终止守卫（TerminationGuard）

五层判定，任一命中即终止：

| 层 | 条件 | 说明 |
|---|---|---|
| L1 | LLM 自我声明完成 | 由解析器处理，不在守卫内 |
| L2 | 假设树收敛 | 所有假设终态 + 队列空 + 无活跃工蜂 |
| L3 | 停滞检测 | 连续 N 轮无新发现确认（默认 3） |
| L4 | 预算守卫 | 超 `max-rounds` / `max-tasks` / `max-wall-time` |
| L5 | 传输层兜底 | httpx 读超时（600s/次）× SDK 重试，挂死请求有界 |

注意 L4 的墙钟判定发生在**轮边界**：单轮内部的长等待由 L5 的传输层超时兜底，而非硬墙。

## 3. Drone（工蜂）

- **原子性**：一个 Drone 只拿一条微任务（"验证 parse_header 的边界检查是否覆盖 length 前缀"），完成后销毁
- **沙盒隔离**：每个任务在独立目录（`tmp/.drone_sandboxes/<task_id>/`）中执行，通过只读软链访问目标
- **多轮 chase**：证据未闭合时可自主追问最多 3 轮（`MAX_CHASE_ROUNDS`），角色化 prompt 覆盖 investigator / evidence-collector / data-flow-tracer / exploit-crafter / harness-generator / crash-analyzer 等
- **会话池**：`session_pool.py` 按角色预热 Session，把冷启动开销从 ~1s 级降到 ms 级

## 4. Blackboard（黑板）

黑板是唯一事实源，持有三类实体：

- `hypotheses`——假设树（含 parent 链、置信度、状态机 pending/active/suspected/confirmed/discarded）
- `tasks`——任务队列（含 drone_role 与假设归属）
- `findings`——确认的漏洞发现

### 4.1 五层语义去重

同一漏洞点被 LLM 换措辞重复提出是常态，入口设五层去重管线（命中即合并而非新建）：

| 层 | 信号 | 阈值 |
|---|---|---|
| L0 | 锚点标识符交集（函数名 / 文件名） | 且词袋重合 ≥ 0.30 |
| L1 | 结构指纹（归一化 token 排序后完全一致） | 精确 |
| L2 | Bigram Jaccard（短语级） | ≥ 0.55 |
| L3 | Unigram Jaccard（词袋级，含安全同义词归一） | ≥ 0.65 |
| L4 | 字符三元组（模糊兜底） | ≥ 0.70 |

合并语义：置信度取 `max(旧, 新) + boost`；被 GC 丢弃的假设可被高置信再提案**复活**。全局版与分区版共享同一实现（`_find_similar_in`），杜绝双份漂移。

### 4.2 置信度与仲裁

- **贝叶斯传播**：`child = 0.6 × (parent × strength) + 0.4 × own`，strength 从历史"父→子"确认样本中 Laplace 学习
- **仲裁**：遇到不可逆操作边界时向人类仲裁队列发起请求；`--auto-approve` 下自动放行（无人值守）

### 4.3 持久化

- 默认 `LocalBackend`：原子写（临时文件 + rename）到 `<work-dir>/.blackboard.json`
- 人类可读轨迹同步追加到 `.audit_notes.md`
- `--resume` 从磁盘完整恢复假设树与任务状态

## 5. Sector（大型目标分区）

超过阈值（文件数或体量）的目标自动进入分区模式：

1. LLM 按目录结构与攻击面把项目分解为 ≤8 个 Sector（失败则启发式按目录名/文件数回退）
2. 每个 Sector 启动独立 Mini-Cerebrum，拥有隔离的 `BlackboardPartition`（假设空间独立，Finding 自动冒泡回主黑板）
3. **动态预算**：随 Sector 优先级缩放（P0 高危区最多、P3 最少），文件数越多预算越高（上限钳制）
4. 全部分区完成后做**跨模块利用链合成**（某个模块的入口 + 另一模块的 sink）

## 6. LLM 接入层

- `engine/llm_config.py`——统一的 Config 构建（`build_llm_config`），base_url / model 从环境变量读取
- **凭证注入**：SDK 每次创建 Session 时都会重读 `KIMI_API_KEY` / `KIMI_BASE_URL` 环境变量并覆盖 Config——因此任何凭证来源（env、外部注入）都无需触碰引擎代码
- `kimi_sdk_compat.py`——启动时打两个补丁：绕过特定 CPython 版本的 typing bug、适配 SDK 的 `skills_dir` 参数签名变化

## 7. 深化闭环

### 7.1 模糊测试

`harness-generator` 角色产出的 `build.sh` 会被交给 `fuzzer.py`：在**断网容器**（`--network=none`、内存/CPU 限额）中编译并运行 libFuzzer，收集 crash 与 ASAN 日志。真实崩溃直接记为 critical finding——这是"LLM 推演 → 真实执行验证"的落地环节。

### 7.2 语义导航

`semantic_helper.py` 为工蜂提供精确的 AST 级导航（tree-sitter）：`list_functions` / `find_references` / `get_call_graph` / `get_function_body` / 数据流追踪。引擎系统提示词（`.bots.md`）规定双通道纪律：**grep 用于发现线索，AST 用于证明调用链**。

### 7.3 依赖漏洞

`osv_bridge.py` 在扫描早期调用 OSV-Scanner（若可用）做依赖清单匹配，结果作为 `dependency_vuln` 类型注入黑板，并压缩为上下文供主脑参考。

## 8. 质量保障体系

- **测试基线**：100 passed / 0 failed。核心语义（去重管线、置信度传播、解析、终止守卫、预算分配）全部有单测锁定
- **假 LLM 集成**：`tests/fake_llm.py` 以脚本化 Session 替换真实 LLM，端到端驱动主循环，任何编排链路回归都会在此暴露
- **可疑点登记册**：`docs/suspicious-registry.md`。审计中发现的疑似缺陷（无论来自静态阅读、测试还是实跑）一律先登记——记录位置、现象、根因、证据、处置状态——**有证据才动手**。确认为 bug 后修复并翻转为回归测试
- **编译交付**：`tools/build_cython.sh` 将 `engine/`、`tools/` 编译为原生 `.so`（交付形态的保护层），同一套 pytest 对源码版与编译版**双跑比对**，保证编译不引入行为偏差
