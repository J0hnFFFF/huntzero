# huntzero 工程化与商业化设计文档

- 日期：2026-07-29
- 状态：已通过分节评审，待用户终审
- 范围：`zerodayFind/`（Python 引擎，现 kimiSec）→ 商业化产品 **huntzero**
- 交付形态：私有化 VM 镜像交付，Web 页面控制

---

## 1. 背景与现状

### 1.1 项目现状

`zerodayFind/` 是一个 LLM 主导的漏洞挖掘引擎（README 自称 kimiSec / HIVE-MIND V8），基于 Moonshot `kimi-agent-sdk`（模型 `kimi-for-coding`），核心架构：

- **Cerebrum 主脑**（`engine/cerebrum.py`，3237 行）：假设驱动推理循环，无工具、输出强制 JSON
- **Drone 工蜂**（`engine/drone.py`）：沙盒化原子执行节点，隔离 Session，最多 3 轮自驱追踪
- **Blackboard 黑板**（`engine/blackboard.py`）：SSOT，五层去重管线 + 贝叶斯置信度引擎
- **Sector 分区**（`engine/sector.py`）：大项目（>5000 文件或 >50MB）自动分区并行 Mini-Cerebrum
- **服务层**（`server/`）：FastAPI 控制台 + MCP 服务 + Redis worker
- **技能库**（`skills/`）：16 个安全领域情报简报，运行时按暴露面路由注入 prompt

引擎能力已被真实扫描验证（对 Ghostscript/ghostpdl 7135 文件的完整 Sector 模式运行，见被清理的运行日志存档）。

### 1.2 关键问题（工程化要解决的）

- git 根目录在 `/home/fuzz/kimiSec`，是"全家桶"单仓：zerodayFind（293 文件）、zdll（383 文件，Go 重写版，已决定放弃）、官网、xray 二进制、运行时产物混在一起
- 配置脱节：`.env.example` 主推的 `ZDLL_LLM_*` 代码零读取；`pyproject.toml` 整体属于另一个项目（clawdboz）；`requirements.txt` 中 `kimi-agent-sdk` 被注释（`requirements.txt:1`），Docker 镜像大概率无法启动
- 死文件/遗留物：`1.md`（416KB 运行日志）、`engine/drone.py.bak2`、`fix_drone_*.py`（一次性补丁脚本）、`security-review.ts`（孤儿参考件）、`notepad_surface.json` 等
- 测试近乎为零：仅 `tests/test_sector.py` 一个文件
- 文档过时：README/AGENTS.md 引用的 `index/`、`knownAttack/`、`model_q/`、`patchdiff/` 在 git 历史中从未存在或不在本仓

### 1.3 zdll 遗产（继续使用的资产）

zdll（Go 重写版）放弃，但其中 50 个已合并 PR 的商业化资产里，**license server 是活资产，继续维护使用**：

- 独立 Go 二进制（`zdll/cmd/license-server/`），HTTP + TLS，文件/Postgres 存储
- Ed25519 签名 license JWT、`GET /v1/llm-credentials` 凭证下发、机器指纹绑定
- 带审计日志和登录锁定的管理 WebUI（厂商侧 license 管理，无需新建）
- 签名 skills vault 更新通道（`/v1/skills-bundle`）
- 协议规范已完整摸清，见附录 A

## 2. 已确认决策

| # | 决策点 | 结论 |
|---|--------|------|
| 1 | 交付形态 | 私有化/本地部署，**VM 镜像密封交付**（非 Docker，**不提供 root/shell**），客户通过 **VM 内 Web 控制台**完成一切操作（激活/扫描/更新/诊断） |
| 2 | LLM 供给 | license server 控制面下发，**Phase 2 直接上 LLM 网关**（主 Moonshot key 不出厂商机房） |
| 3 | 推进顺序 | 先内功（清理/配置/测试）后商业化；测试先行，不重构 cerebrum 结构 |
| 4 | 仓库 | `git filter-repo` 拆出独立产品仓，remote = `https://github.com/J0hnFFFF/huntzero` |
| 5 | 品牌 | **huntzero**（沿用 zdll 商业品牌，license server 体系零改动） |
| 6 | 开源策略 | **不开源**，改专有 EULA（现 README 的 MIT 声明需替换） |
| 7 | OS 基底 | **Ubuntu Server LTS minimal（无桌面）**；Raspberry Pi / arm64 进 roadmap，不在本期 |
| 8 | 行为改动纪律 | 不修静态推断的"bug"；可疑点进登记册 + 表征测试钉现状，逐项业务分析后另行立项 |
| 9 | 范围与时间 | Phase 2 不裁剪，无紧迫时间线 |
| 10 | `docs/weaponization_flowchart*.md` | 保留，归入 `docs/design/` |

## 3. 总体路线

```
Phase 0  拆仓（~1 天）
   zerodayFind/ → 独立产品仓 huntzero（github.com/J0hnFFFF/huntzero）
        │
Phase 1  内功（~2 周量级）
   1a 清理死文件      1b 配置体系统一
   1c 测试体系        1d 品牌统一 + Nuitka 打包 spike
        │
Phase 2  商业化（~2 周量级）
   2a license 客户端 + LLM 网关
   2b 交付物保护 + VM 镜像流水线
   2c 部署/操作/运维/安全白皮书文档
```

### 非目标（明确不做）

- 不重构 `cerebrum.py` 内部结构（3237 行编排核心，零测试现状下动结构 = 赌已验证行为）
- 不修任何静态推断的"bug"（见 §8 可疑点登记册）
- 不动引擎扫描行为与 prompt 体系
- 不做 SaaS / 多租户 / Web 控制台多用户 RBAC
- 不做 arm64 / Windows / macOS 交付物
- 不删 zdll 源码（旧仓归档，license server 继续维护）

### 成功标准

- **Phase 0**：新仓 clone 干净、zerodayFind 相关历史完整、`pytest` 可跑；旧仓打 `archive-pre-split` tag 转只读
- **Phase 1**：pytest 全绿覆盖 §5.3 清单；fake-LLM 集成测试在 fixture 项目上跑通主循环；`.env.example` 与代码读取点一一对应（CI 校验）；仓库无死文件；Nuitka spike 产出能跑冒烟的二进制
- **Phase 2**：无效/吊销 license → 引擎拒绝启动；有效 license → 凭证经网关自动注入跑通真实扫描；对真实 license server 的协议一致性测试全过；Packer 一键产出 QCOW2 + OVA；干净虚拟化环境按部署手册走通"导入 → Web 激活 → 扫描 → 出报告"

**实施方式**：本设计为总纲；实施计划按 Phase 分别制定（每个 Phase 一份独立实施计划），Phase 0 → 1 → 2 串行推进。

## 4. Phase 0：拆仓（约 1 天）

1. 新鲜 clone `/home/fuzz/kimiSec`，执行 `git filter-repo --subdirectory-filter zerodayFind`，产物为独立仓，历史完整保留，目录内容提升为仓库根
2. 重写 `.gitignore`：`__pycache__/`、`.blackboard*.json`、`.audit_notes.md`、`local_workspace/`、`tmp/`、`bin/osv-scanner*`、`*.vault`、`.env`
3. 接入新 remote `https://github.com/J0hnFFFF/huntzero` 并推送
4. 旧仓打 `archive-pre-split` tag，转只读归档；zdll 与 license server 继续在旧仓维护
5. 验证：新仓 `pytest`、`python kimi.py --help` 可跑（配置问题此阶段不修）

注意：本设计文档位于 `zerodayFind/docs/` 下，拆仓时随产品历史一并进入新仓。

## 5. Phase 1：内功

### 5.1 Phase 1a：清理（纯删除/搬迁，零行为风险）

**直接删除**（已 grep 验证零引用）：

| 文件 | 原因 |
|---|---|
| `1.md` | 416KB 运行日志存档 |
| `engine/drone.py.bak2` | 补丁前备份 |
| `fix_drone_resource_leak.py`、`fix_drone_v2.py` | 一次性字符串替换补丁脚本，补丁已应用 |
| `security-review.ts` | 从 Claude Code 源码摘出的孤儿参考件，import 路径不存在 |
| `notepad_surface.json` | 误提交的运行产物 |
| `go_vs_python_comparison.md`、`docs/python_vs_go_comparison.md` | zdll 时代对照文档，已过时 |
| `web_ui/index.html` | 旧单页控制台，后端不服务它（`server/http.py` 只读 `web_ui/dist`）；保留 `web_ui/` 作为 v2 构建输出目录 |
| `requirements-worker.txt` | 全仓零引用的死文件，内容本身也有问题（缺 `redis[asyncio]`） |
| `bin/osv-scanner.exe` | 56MB Windows 二进制；改为构建期按平台下载（`tools/install_osv.py` 已有此能力） |
| `__pycache__/`、`.blackboard.json`、`.attack_blackboard.json`、`.audit_notes.md`（根）、`local_workspace/`、`tmp/` 内容 | 运行时产物，untrack + gitignore |
| `.workbuddy/`、`.cloudstudio` | 工具状态目录，不入产品仓 |

**移出产品仓**（relocate 到旧仓/营销物料处，不是删除）：

- `frontend/` + `artifacts/` + `docs/` 的 PPT 流水线（`pages.json`、`project.json`、`page-global-config.json`、`product/`）与实战文章《当我们用AI挖到第一个高危——假设驱动认知审计实战.md》
- `vuln-hunting-plugin/`（上游开源可得 `github.com/lielingxyz/vuln-hunting-plugin`，产品代码零引用）
- `sync_intel.py`、`sync_wildfire.py`（厂商侧官网运营工具，属 `index/` 网站体系，不随产品走）
- `docs/weaponization_flowchart*.md` → 保留，移入 `docs/design/`

**审查后决定**（确认无引用再动）：

- `skills/scheduler`、`skills/find-skills`：非安全领域 skill；确认 `skills/security-expert/SKILL.md` 路由表与 `engine/cerebrum.py` 不引用后删除
- `engine/semantic_helper.py` 与 `tools/semantic_helper.py` 重复：`.bots.md:34` 指向 engine 版（drone 实际调用），tools 版功能更强（污点追踪，~750 行）。**先写表征测试，再合并功能并集到 engine 版**，删 tools 版，更新 README 引用

### 5.2 Phase 1b：配置体系统一

**环境变量规范**（保留 `KIMI_*` 命名——SDK 的 env override 通道 `kimi_cli/llm.py:56-93` 只认这套名）：

| 变量 | 用途 | 状态 |
|---|---|---|
| `KIMI_API_KEY` | LLM key（开发模式直读；license 模式下由客户端注入） | 保留 |
| `KIMI_BASE_URL` | LLM 端点（license 模式下指向网关） | 保留 |
| `KIMI_REDIS_URL` | Redis 地址，决定 local/cluster 模式 | 保留 |
| `KIMISEC_API_KEY` | HTTP REST/WS 认证 | 保留 |
| `KIMI_REPORTS_DIR` | Redis 后端审计落盘目录 | 保留 |
| `KIMI_DOC_INTEL_TIMEOUT` | Round 0 文档情报超时 | 保留 |
| `OSV_SCANNER_PATH` | osv-scanner 路径 | 保留 |
| `HUNTZERO_LICENSE_KEY` / `HUNTZERO_LICENSE_SERVER_URL` / `HUNTZERO_LICENSE_PUBLIC_KEY` | license 客户端（Phase 2 新增） | 新增 |
| `ZDLL_LLM_*`（4 个）、`KIMI_WORKERS`、`INTEL_SECRET`、`WECOM_WEBHOOK` | 幽灵变量，代码零读取 | 从所有文档/部署文件清除 |

**具体动作**：

1. 重写 `.env.example`：与代码读取点逐一对应，每行注释；配 CI 校验脚本（grep 代码 env 读取点 vs `.env.example`，防再脱节）
2. 重写 `pyproject.toml`：`name="huntzero"`、真实依赖（**`kimi-agent-sdk` 必须进依赖**，修 `requirements.txt:1` 被注释的根因）、CLI entry `huntzero = "huntzero:main"`（配合 §5.4 改名）、`requires-python=">=3.11"`，保留 black(100列)/ruff/mypy/pytest 工具段；原 clawdboz 内容全部清除
3. 依赖声明收敛为单一口径：`pyproject.toml` 为唯一权威，`requirements.txt` 改为由它导出（或删除只留 pyproject），`requirements-worker.txt` 已删（§5.1）
4. 合并两处逐字重复的 `build_config`（`kimi_hive.py:678-693` 与 `server/core.py:363-381`）为公共函数（落点 `engine/llm_config.py`），base_url/model 从 env 读，默认行为不变
5. `docker-compose.yml` / `Dockerfile` 中的幽灵变量插值清理（构建继续用 Docker，仅交付不用）

### 5.3 Phase 1c：测试体系

**纯逻辑单测**（pytest + pytest-asyncio 已配置）：

| 模块 | 覆盖点 |
|---|---|
| `engine/blackboard.py` | 五层去重管线（锚点/结构指纹/Bigram/Unigram/Trigram 各层边界）、`BayesianConfidenceEngine`、仲裁队列、`BlackboardPartition` 冒泡汇聚 |
| `engine/backends.py` | `LocalBackend` 原子写、`IncrementalLocalBackend`（WAL/compaction/MD5 变化检测）、`RedisBackend`（fakeredis） |
| `engine/cerebrum.py` | `_parse_output`/`_parse_json_output`（脏 JSON、截断、多对象、markdown 包裹） |
| `engine/sector.py` | LLM 分区失败后的启发式回退（`sector.py:395-482`）、动态预算分配 |
| `engine/cerebrum.py` | `TerminationGuard` 五层判定（`cerebrum.py:340-419`） |
| `engine/osv_bridge.py` | OSV JSON 解析、Finding 转换、二进制缺失静默跳过 |
| `tools/exploit_analyzer.py` | 静态规则层（LLM 缺省回退路径） |

**fake-LLM 集成测试**（最高价值资产）：

- 仿 `kimi_sdk_compat.py` 的手法 patch `Session.create`，注入脚本化 `FakeSession`（按调用序列返回预设 JSON 响应）
- fixture 项目：~5 个文件、故意埋 1-2 个漏洞的小应用
- 端到端断言：假设生成 → 任务分发 → drone 执行 → critic 审查 → finding 落黑板 → 报告生成（Markdown/JSON）→ 终止条件触发

**表征测试 + 可疑点登记册**（§8）：钉住 `drone.py:370` `_cleanup()` 缩进行为、`server/http.py` `_validate_project_path` 的 `Path` 引用、FinOps 模型名现状。

**门槛**：不设虚荣覆盖率数字；门槛 = 上表清单全覆盖 + 集成冒烟全绿。

### 5.4 Phase 1d：品牌统一 + Nuitka spike

- 品牌替换点：`.bots.md`（标题"kimiSec V8"）、README、CLI prog 名、MCP server 标识、`web_ui_v2` 控制台标题、报告落款
- `kimi.py` → `huntzero.py`（新仓新开始，不留兼容 shim）；`KIMI_*` 环境变量名不动（SDK 契约）
- pip 包名 `huntzero`；README 的 MIT 声明替换为专有 EULA 占位（法律文本后补）
- **Nuitka spike**（打包命门，提前验证）：编译 `huntzero.py` + 全依赖为 standalone 二进制；验证项 = build 通过、二进制 `--help` 可用、对 fixture 项目跑通冒烟扫描、同一套 pytest 可对着编译产物跑。任一关键项失败 → 落 Cython fallback（`engine/`、`server/`、licensing 编译为 `.so` + python-build-standalone 便携运行时）

## 6. Phase 2：商业化

### 6.1 Phase 2a：License 客户端（新增 `huntzero/licensing/` 包）

按附录 A 协议规范实现，要点：

1. **激活验证**：`POST /v1/verify`，载荷含 `license_key`、`machine_fp`（**与 Go 版逐字节一致**：`/var/lib/dbus/machine-id` → `/etc/machine-id` 回退，`sha256("{goos}|{goarch}|{machine_id}|{hostname}")[:16].hex()`，避免同机占两个名额）、`cli_version`、`skills_build`
2. **JWT 校验**：PyJWT 强制 `algorithms=["EdDSA"]`，验签 + `exp` + `machine_fp` 绑定比对；公钥来自 `HUNTZERO_LICENSE_PUBLIC_KEY` env 或编译期内嵌
3. **凭证获取**：`GET /v1/llm-credentials`（Bearer JWT），验 Ed25519 签名（换行拼接载荷、UTC 秒级 RFC3339、canonical JSON `extra_body`），**fail-closed**：无签名/验签失败/网络失败 → 拒绝启动
4. **凭证注入**：写 `os.environ["KIMI_API_KEY"]` / `["KIMI_BASE_URL"]`（指向网关）。注入点在 `get_api_key()`（现 `kimi.py:26-32`，Phase 1d 改名后为 `huntzero.py`）咽喉处：有 `HUNTZERO_LICENSE_KEY` 走 license 流程；否则回退裸 `KIMI_API_KEY`（**开发模式**，打警告日志）。SDK 每次 `Session.create` 重读 env（`kimi_cli/llm.py`），三条调用链（CLI/serve/worker）零改动生效
5. **长驻进程刷新**：serve/worker 模式起后台协程每 ~30min 重写 env（凭证 1h TTL；JWT 24h 到期重新 verify）。已存活 session 不刷新（与 Go 行为对齐；池内死 session 重建时自然读到新值）
6. **skills vault**：启动时按 verify 响应的 `update` 信息下载/验签 vault，用 JWT `claims.dek`（AES-256-GCM）解密到 tmpfs（`/dev/shm` 或 `tmpfs` 挂载点），cerebrum 的 skills 加载路径指向解密区；`min_cli_version` 不满足时提示不更新不中断（对齐 Go 行为）
7. **协议一致性测试**：测试栈起真 license server（Go 二进制），跑 verify → creds → vault 全链路 + 吊销/过期/限流/改签名等负路径
8. 新增依赖：`PyJWT[crypto]`、`cryptography`

### 6.2 LLM 网关（Phase 2 直接上）

**动机**：凭证下发模式下客户 VM 内是 root，可提取主 Moonshot key；多客户共享主 key = 配额滥用不可归因不可吊销。网关让主 key 永不出厂商机房。

**设计**：

- 轻量 OpenAI 兼容反向代理，**v1 内嵌为 license server 进程内模块**（同进程同库，查吊销零额外调用，复用同一 Ed25519 密钥体系与存储后端）；不做独立部署形态
- 客户端 `KIMI_BASE_URL` 指向网关、`KIMI_API_KEY` = license JWT：凭证下发流程零改动——license server 的 `llm_base_url` 配置为网关地址，`/v1/llm-credentials` 响应的 `api_key` 字段直接承载 license JWT；网关用同一公钥验签 + 同库查吊销 → 转发 Moonshot（`api.kimi.com/coding/v1`）→ 按 license 计量 token 用量
- 吊销即时生效（网关侧拒绝）；计量数据支撑计费与合规审计
- 网关对 SDK 透明：仅做 HTTP 转发 + 认证 + 计数，不改写载荷语义

**范围控制**：v1 只做 chat completions 转发 + 验签 + 计量；不做缓存、不做多上游负载均衡。

### 6.3 交付物保护（三层模型）

| 层 | 对象 | 手段 |
|---|---|---|
| 1 | 引擎代码（engine/server/licensing） | Nuitka standalone 编译（spike 验证，fallback Cython `.so`），VM 镜像内无源码 |
| 2 | skills 知识库（核心 IP） | vault 加密交付（AES-256-GCM），DEK 随 license JWT 下发，运行时仅解密到 tmpfs，磁盘永存密文 |
| 3 | license 绕过 | 锚定在控制面：删客户端检查 = 断 LLM 供给（网关验签 + 计量 + 吊销）；配套专有 EULA 合同约束 |

Phase 1 测试体系双跑（源码版 + 编译版同一套 pytest），保证编译不引入行为偏差。VM 密封交付（无 root/shell）在第 1、2 层之上再加一道门槛；残余风险为客户离线挂载磁盘镜像逆向，由第 3 层与合同兜底。

### 6.4 Phase 2b：VM 镜像流水线（Packer）

**基底**：Ubuntu Server 24.04 LTS minimal（无桌面），cloud-init 注入统一安装脚本。

**镜像内容**：

- huntzero Nuitka 二进制（无源码）
- `redis-server`（apt 包，本机服务）
- nginx：TLS 终结（初始自签 + 客户证书替换流程）+ Web 控制台静态文件（`web_ui_v2` 构建产物）+ 反代 `/api`、`/ws`
- Docker engine（fuzzer 隔离与 PoC 沙箱用，`kimisec-fuzzer` 镜像预置加载）
- osv-scanner Linux 二进制
- skills.vault（密文）
- systemd 单元：`redis`、`huntzero-server`（FastAPI+MCP）、`huntzero-worker`（默认 1，可扩）、`nginx`
- **密封交付**：不提供 root/shell（sshd 不对外开放）；Web 控制台是客户唯一交互面。首启网络 DHCP 默认，静态 IP/主机名在控制台网络设置页配置
- **Web 激活门**：未激活时控制台仅呈现激活页（输入 license key；license server 地址内嵌默认值、可改），后端验证通过后把 key 持久化到数据卷（`/data/huntzero/license.env`，systemd `EnvironmentFile=` 加载，与 §6.1 的 env 注入路径对齐），解锁完整控制台

**Web 控制台**（`web_ui_v2` 产品化）：

- 定位：huntzero 操作界面（提交目标、job 监控、findings 浏览、报告下载、工作区），**不含 license 管理功能**（厂商侧用 license server 已有 WebUI）
- 认证硬化：首启设置管理员密码 → 会话 token（v1 单管理员，不做 RBAC）；替代现裸 `X-API-Key`
- 密封运维能力：诊断日志查看与支持包下载（工单用）、引擎更新包上传与应用、网络设置页——全部 Web 内完成，无需 shell

**产物**：x86_64 QCOW2 + OVA 双格式，版本号打标。资源基线文档化：最低 4 vCPU/8GB/40GB，建议 8 vCPU/16GB（大项目 Sector 模式）。

**更新策略**：skills vault 走 license 订阅通道自动更新；引擎版本发签名更新包（Web 控制台上传应用）；大版本发新镜像。

### 6.5 Phase 2c：文档

- **部署手册**：镜像导入（VMware/KVM/VirtualBox）、资源规格、网络出方向要求（HTTPS 到 license server 与 LLM 网关）、DHCP/静态 IP 配置（Web 网络设置页）、Web 首启激活、防火墙建议
- **操作手册**：Web 控制台使用、扫描参数建议、报告解读
- **运维手册**：密封 appliance 运维（Web 诊断页、支持包下载、升级流程、blackboard 备份）、厂商侧远程支持流程、故障排查
- **安全白皮书**：凭证流向、隔离模型、数据出向、知识库加密——安全厂商客户采购必审
- README 重写（产品定位 + 快速开始）、AGENTS.md 重写（面向贡献者的仓库现状）

## 7. 风险登记

| 风险 | 影响 | 缓解 |
|---|---|---|
| Nuitka 与 kimi-agent-sdk 动态导入不兼容 | 交付物保护路线受阻 | Phase 1 末 spike 兜底，fallback Cython |
| fake-LLM patch 点随 SDK 升级失效 | 集成测试脆性 | pin `kimi-agent-sdk==0.0.5`（kimi-cli 1.12.0），升级必跑集成测试 |
| license server 已知小瑕疵（`token_ttl` 配置不生效恒 24h、JWT 无 `nbf`） | 协议行为偏差 | 不改协议，客户端兼容；瑕疵记录待旧仓另行修 |
| 网关增加一个线上服务的开发运维 | Phase 2 工作量增加 | 复用 license server 密钥/存储，控制范围在 v1 最小集 |
| LLM 凭证/网关可用性成为单点 | 客户扫不了 = 投诉 | 网关无状态可水平扩；license server 文件后端可备份；文档写清 SLA 边界 |
| 密封 appliance 客户无法自查现场问题 | 支持成本升高 | Web 诊断页 + 支持包下载；激活页展示机器指纹与 license 状态便于工单定位 |
| 表征测试钉住的行为日后被证明是 bug | 登记册积压 | 登记册逐项业务分析立项，不占本期范围 |

## 8. 可疑点登记册（不修原则）

原则：**任何行为改动需要精准的上下文和深入业务分析；静态推断的"疑似 bug"一律先登记 + 表征测试钉现状，逐项另行立项。**

初始登记项：

| 位置 | 现象 | 备注 |
|---|---|---|
| `engine/drone.py:370` | `self._cleanup()` 缩进在 `async with` 块内，疑似每轮循环触发 | 可能是刻意的防泄漏设计，需业务分析 |
| `server/http.py` `_validate_project_path` | 返回注解用 `Path` 但文件未 import | 运行时才触发 NameError，需确认调用路径 |
| `kimi_hive.py:852` 附近 | FinOps 成本统计模型名（"kimi-long-context"/"kimi-latest"）与实际 `kimi-for-coding` 不符 | 成本估算偏差 |
| `engine/semantic_helper.py` vs `tools/semantic_helper.py` | 功能重复，`.bots.md:34` 指 engine 版，tools 版功能更强 | 见 §5.1 合并方案，先表征测试 |
| `kimi.py` `feed` 子命令 | 空转占位（TODO: GitHub auto-feed） | 保留或删除另行决定 |

## 9. Roadmap（本期之外）

- Raspberry Pi / arm64 appliance 镜像（Ubuntu Server for Pi，Pi 5 + NVMe；定位 PoC 演示与中小型目标）
- Web 控制台多用户 / RBAC
- SaaS 形态（如商业模式演变）
- `cerebrum.py` 结构重构（在测试保护下的三期工程）
- 可疑点登记册逐项业务分析与修复立项

## 10. 已解决的开放问题

- ~~LLM 网关时机~~ → Phase 2 直接上（§6.2）
- ~~新仓 remote~~ → `https://github.com/J0hnFFFF/huntzero`
- ~~开源策略~~ → 专有 EULA，不开源
- ~~Raspberry Pi~~ → 进 roadmap，本期不做
- ~~weaponization 文档~~ → 保留入 `docs/design/`
- ~~Phase 2 裁剪~~ → 不裁剪，无紧迫时间线

---

## 附录 A：License 协议规范摘要（实现依据，源自 zdll 源码）

**端点**（客户端只碰 3 个，均有 per-IP 令牌桶限流 10rps/burst20）：

- `POST /v1/verify`：请求 `{license_key, machine_fp(32hex), cli_version?, skills_build?}`；错误为**纯文本**（400/403/404/409）；成功返回 `{token: JWT, update?: {...}}`
- `GET /v1/llm-credentials`：Bearer JWT；错误为 **JSON** `{"error":...}`；成功返回 `{base_url, api_key, model, provider, expires_at(RFC3339Nano), extra_body?, signature(hex Ed25519)}`，`expires_at` = 服务端 now+1h
- `GET /v1/skills-bundle`：Bearer JWT；直发（200 octet-stream + `X-Skills-Version`/`X-Skills-Signature` 头）或 302 CDN（验签回退用 verify 响应的 `update.skills_signature`）；100MiB 下载上限

**JWT**：EdDSA，claims = `{license_key, features[], dek(64hex), machine_fp, exp, iat}`；`exp = min(license.expires_at, now+24h)`；无 `nbf`/`kid`/`iss`。

**机器指纹**：`seed = goos + "|" + goarch + "|" + machine_id + "|" + hostname`；`machine_fp = sha256(seed)[:16].hex()`；machine_id 读 `/var/lib/dbus/machine-id`（回退 `/etc/machine-id`）trim；goos 映射 linux/darwin/windows，goarch 映射 amd64/arm64。首次 verify 自动绑定机器，默认 max_machines=2，超出返回 `409 too many machines`。

**凭证验签载荷**（逐字节复刻）：

```
base_url + "\n" + api_key + "\n" + model + "\n" + provider + "\n" + expiresAt.UTC()("YYYY-MM-DDTHH:MM:SSZ") [+ "\n" + canonicalJSON(extra_body)]
```

canonicalJSON = `json.dumps(obj, sort_keys=True, separators=(",",":"), ensure_ascii=False)`（注意 Go 对 `<>&` 的 HTML 转义与浮点格式差异，当前实际内容不受影响）。

**skills vault 签名**：`payload = skills_version + "\n" + skills_url + "\n" + hex(sha256(bundle_bytes))`，Ed25519 验签；`skills_url` 用 verify 响应里的原值（非最终下载 URL）。

**vault 格式**：JSON `{version:1, build, files:{path:{nonce(b64,12B), ciphertext(b64, ct‖16B tag)}}}`；AES-256-GCM，key = JWT `claims.dek` hex 解码 32 字节。

**客户端行为基线**（对齐 Go）：纯在线无缓存、无宽限、无心跳；启动 = 1×verify + 1×llm-credentials（+ 可选 1×skills-bundle）；失败即拒绝启动；vault 更新失败仅记日志不中断；`min_cli_version` 不满足时提示、不更新、不中断；服务端 `update.auto_update` 字段客户端不读，决策看本地配置。

## 附录 B：代码改造锚点速查

| 改造项 | 落点 |
|---|---|
| license 凭证注入 | `kimi.py:26-32` `get_api_key()`（三条路径共同咽喉） |
| `build_config` 合并 | `kimi_hive.py:678-693` + `server/core.py:363-381` → `engine/llm_config.py` |
| env 一致性 CI 校验 | 新脚本扫 `os.environ`/`os.getenv` 读取点 vs `.env.example` |
| SDK env override 通道 | `kimi_cli/llm.py:56-93`（每次 Session.create 重读 env，license 注入因此零侵入生效） |
| session 池凭证时效 | 池仅存在于 CLI scan 路径（`kimi_hive.py:751-761`），与 job 同生命周期，可接受 |
| worker 凭证刷新 | 后台协程定期重写 env；新 job 新 session 自动生效 |
| skills 加载路径 | `engine/cerebrum.py:1037` `_load_domain_terrain()`、`:1056` `_build_domain_pipeline()` → 指向 vault 解密区 |
| Web 控制台认证 | `server/http.py:44,402`（`X-API-Key`）→ 首启密码 + 会话 token |
| fuzzer Docker 依赖 | `engine/fuzzer.py`（VM 内预置 Docker，无需降级逻辑，但保留探测保护） |
