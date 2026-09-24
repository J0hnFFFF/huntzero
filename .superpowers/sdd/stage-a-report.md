# huntzero Rebuild — Stage A 报告（清理 + 配置统一）

日期：2026-09-25 ｜ 基线：ef1c771 ｜ 执行：subagent（auto mode）

## Commits

| SHA | 内容 |
|---|---|
| 9f114b6 | chore: remove dead files and out-of-scope code (SPEC 5.1) |
| fdded32 | build: rewrite pyproject/gitignore for huntzero pure-CLI direction (SPEC 5.2) |
| 473cd8b | feat(config): rewrite .env.example and add env sync checker (SPEC 5.2) |

工作树干净（runtime 产物已被新 .gitignore 覆盖，磁盘上保留但未跟踪）。

## 删除清单核对

### 计划 §5.1 死文件（全部 git rm，删前 grep 零引用）

- [x] 1.md、engine/drone.py.bak2、fix_drone_resource_leak.py、fix_drone_v2.py（仅互相引用，一并删）
- [x] security-review.ts、notepad_surface.json、go_vs_python_comparison.md、docs/python_vs_go_comparison.md
- [x] web_ui/（整目录）、requirements-worker.txt、bin/osv-scanner.exe（bin/ 随之消失）
- [x] frontend/、artifacts/、vuln-hunting-plugin/、sync_intel.py、sync_wildfire.py
- [x] 《当我们用AI挖到第一个高危——假设驱动认知审计实战.md》
- [x] docs/pages.json、docs/project.json、docs/page-global-config.json、docs/product/
- [x] skills/scheduler/、skills/find-skills/ —— 引用检查：cerebrum.py:1152 仅为解释 skills_dir=None 的注释；skills/counter/references/orchestrator.md:202 的 "scheduler:" 是 PoC YAML 示例内容。均无真实引用，安全删除。
- [x] untrack：.blackboard.json、.attack_blackboard.json、.audit_notes.md、local_workspace/、tmp/、__pycache__/、.workbuddy/、.cloudstudio
- [x] 移动：docs/weaponization_flowchart_{optimized,text}.md → docs/design/（git mv，保留历史）

### 方向外代码（最终方向：纯 CLI 扫描器）

- [x] server/ 整目录删除（http/mcp/worker/core/__init__）
- [x] docker-compose.yml、CLUSTER_GUIDE.md、Dockerfile（Dockerfile.fuzzer 保留）
- [x] engine/backends.py 手术：删除 RedisBackend、IncrementalLocalBackend 类；make_backend 工厂调用方 grep 结果仅剩 server/worker.py（已删）与 engine/__init__.py 的 re-export（已同步）→ 工厂一并删除；redis import 随类删除；模块 docstring 重写。保留 StorageBackend + LocalBackend。
- [x] engine/__init__.py：导出收敛为 StorageBackend/LocalBackend
- [x] engine/blackboard.py:417-420 docstring 同步（去掉 RedisBackend 条目）
- [x] kimi.py：删除 serve/worker 子命令（argparse 块、cmd_serve/cmd_worker、dispatch、帮助文本）；**另删除 feed 与 binrev 子命令**——"只留 scan" 的要求下二者亦须移除：feed 依赖已删的 server.core，binrev 依赖仓内不存在的 binrev 模块（本就 broken）
- [x] .env.example：只保留 KIMI_API_KEY（必需）/KIMI_BASE_URL/KIMI_DOC_INTEL_TIMEOUT/OSV_SCANNER_PATH；删除 KIMI_REDIS_URL/KIMI_REPORTS_DIR/KIMISEC_API_KEY/KIMI_WORK_DIR/ZDLL_LLM_*；并移除 license/VM 相关注释（方向外）

## 方向修正点（相对计划原文的偏离）

1. pyproject.toml：dependencies 删除 redis[asyncio]/fastapi/uvicorn[standard]/pydantic（grep 确认：uvicorn 仅出现于已删的 cmd_serve；fastapi/redis 在 engine/ 中仅是 cerebrum 技术栈关键词字符串；pydantic 无使用点）。packages.find include 只留 engine*/tools*；py-modules 只留 kimi/kimi_hive/kimi_sdk_compat；[project.scripts] huntzero = "kimi:main"。
2. requirements.txt 同步为去掉四个方向外依赖的导出快照。
3. .gitignore 在计划全文基础上加 `.attack_blackboard.json` 一行。
4. .env.example 头部注释删去 "license 注入/VM 交付" 表述。

## 验证输出

1. `import kimi_hive, engine.cerebrum, engine.drone, engine.blackboard, engine.backends` → **imports OK**
2. `kimi.py --help` → 子命令只剩 `{scan}`；`kimi.py scan --help` 正常（11 个参数齐全）
3. `pytest tests/ -q` → **16 passed, 1 failed**。失败为 test_sector.py::test_hypothesis_dedup_within_partition，**与基线（ef1c771）完全相同的预存失败**，布隆修复在阶段 B，符合预期。新增 test_env_sync.py PASS；`python tools/check_env_sync.py` 输出 `env sync OK`
4. ruff：基线 490 errors → 现 278 errors（净减 212，全部来自删文件）。改动文件中零新增：engine/backends.py 7→3、engine/__init__.py 1→1、kimi.py 2→0、engine/blackboard.py 36→36；新文件 check_env_sync.py / test_env_sync.py 为 0。残留 4 条（I001/UP045）为原代码逐字保留的既有风格问题，与全仓存量一致。
5. 残留 grep（redis|fastapi|uvicorn|web_ui|server\.）逐条判断：
   - engine/cerebrum.py:954,977 —— 技术栈关键词字符串字面量（"fastapi"/"redis" 作为目标技术名），非依赖，保留正确
   - .dockerignore:32-34 —— web_ui 构建产物忽略行；文件本身因 Dockerfile 删除而孤儿化，无害（见 follow-up）
   - pyproject.toml:32 —— fakeredis（dev extra），见 follow-up
   - README.md / AGENTS.md —— 描述旧架构（server/、redis、web_ui），计划 Task 17（阶段 D）重写，预期内
   - skills/kimisec-hive/SKILL.md:15-17 —— 声明指向不存在文件 kimi_mcp_server.py 的 mcp_server（cerebrum 已用 skills_dir=None 规避加载），阶段 B/C 处理 skill 时再校
   - skills/*/references/*.md —— 安全 playbook 中的目标技术示例内容，合法保留
   - Python 代码中 `from server / import server / server.core` 等模块引用：**零残留**

## Follow-ups（未做，供后续阶段决策）

1. **web_ui_v2/ 仍被跟踪**——不在本阶段删除清单内，但它是 Web 控制台，与"纯 CLI"终态冲突，疑似清单遗漏，建议下阶段删除。
2. pyproject dev extra 中 fakeredis>=2.20 服务于已删的 RedisBackend 测试，可裁。
3. .dockerignore 已无配套 Dockerfile，可删（其 web_ui 引用随之消失）。
4. cerebrum.py:1152 注释仍提及 scheduler/find-skills 作为 skills_dir=None 的例证，轻微过时但论据仍成立。
