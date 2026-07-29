# huntzero Phase 0+1（拆仓与内功）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 `zerodayFind/` 从全家桶单仓拆为独立产品仓 huntzero，并完成清理、配置统一、测试体系、品牌统一与 Nuitka 打包验证。

**Architecture:** 遵循设计文档 `docs/superpowers/specs/2026-07-29-huntzero-productionization-design.md`（下称 SPEC）§4-§5。只做删除/搬迁/配置/测试/改名，不改引擎扫描行为；静态推断的疑似 bug 一律进 `docs/suspicious-registry.md` 并用表征测试钉住现状。

**Tech Stack:** Python 3.11+、pytest + pytest-asyncio（auto 模式）、kimi-agent-sdk 0.0.5（pin）、black(100列)/ruff(E,F,W,I,N,UP)、git filter-repo、Nuitka。

## Global Constraints

- 设计文档：`docs/superpowers/specs/2026-07-29-huntzero-productionization-design.md`（SPEC），一切范围以它为准
- **禁止行为改动**：除本计划明确列出的删除/移动/配置/新增测试/改名外，不修改 `engine/`、`server/`、`kimi_hive.py` 的任何逻辑
- 疑似 bug 不修，进 `docs/suspicious-registry.md`（SPEC §8）
- `KIMI_*` 环境变量名不动（kimi-agent-sdk 的 env override 契约）
- 测试约定（对齐 `tests/test_sector.py`）：async 测试直接 `async def` 不加装饰器（`asyncio_mode=auto`）；fixture 用同步 `@pytest.fixture` + `tmp_path`；纯 `assert`；类间用 `# ───` 分隔注释；允许直接测私有方法
- 提交规范：conventional commits（`chore:`/`test:`/`docs:`/`refactor:`/`build:`），每个 Task 末尾按步骤提交
- Python 环境：`/home/fuzz/miniconda3/envs/sec`（conda env `sec`，Python 3.13.5），命令前缀 `python` 均指该环境；需先 `pip install -e ".[dev]"`（Task 6 完成前用 `pip install pytest pytest-asyncio` 临时满足）
- 新仓位置：`/home/fuzz/huntzero`（Task 1 产出），Task 2 起所有工作在新仓进行
- 旧仓：`/home/fuzz/kimiSec`（git root），归档用途，不向它 push

---

### Task 1: Phase 0 — 拆仓（filter-repo）

**Files:**
- 产出：新仓 `/home/fuzz/huntzero`（zerodayFind 子目录内容提升为根）
- 修改：旧仓 `/home/fuzz/kimiSec`（打 tag，不改内容）

**Interfaces:**
- Produces: `/home/fuzz/huntzero` git 仓库，含 zerodayFind 全部历史；后续所有 Task 在此目录执行

- [ ] **Step 1: 确认工具与现状**

```bash
pip show git-filter-repo || pip install git-filter-repo
cd /home/fuzz/kimiSec && git status --short | head -5 && git log --oneline -1
```

预期：`git-filter-repo` 可用；工作区仅有已知的未跟踪文件（如 `zdll/license-server2`），无未提交的 zerodayFind 修改（除 spec 已提交）。

- [ ] **Step 2: 新鲜 clone 并执行拆分**

```bash
git clone /home/fuzz/kimiSec /home/fuzz/huntzero
cd /home/fuzz/huntzero
git filter-repo --subdirectory-filter zerodayFind
```

预期：仓库根变成原 `zerodayFind/` 的内容（`engine/`、`server/`、`kimi.py` 等在顶层）；`git log --oneline | wc -l` 显著小于原仓（只保留触碰过 zerodayFind 的提交）。

- [ ] **Step 3: 验证新仓可用**

```bash
cd /home/fuzz/huntzero
ls engine/ server/ skills/ kimi.py README.md
python -m pytest tests/ -q
```

预期：目录结构正确；pytest 能跑（`test_sector.py` 应通过；若因缺 pytest 失败，先 `pip install pytest pytest-asyncio`）。

- [ ] **Step 4: 旧仓打归档 tag**

```bash
cd /home/fuzz/kimiSec && git tag archive-pre-split && git tag -l 'archive*'
```

预期：输出 `archive-pre-split`。**不 push**（旧仓此后只读归档；zdll 与 license server 继续在旧仓维护）。

- [ ] **Step 5: 关联新 remote（用户手动确认后执行 push）**

```bash
cd /home/fuzz/huntzero
git remote remove origin
git remote add origin https://github.com/J0hnFFFF/huntzero.git
git remote -v
```

预期：remote 指向新地址。**push（`git push -u origin main`）需要用户的 GitHub 凭证与仓库已创建——这一步由用户执行或明确授权后执行。**

- [ ] **Step 6: Commit**

无需提交（filter-repo 已重写历史）。在任务跟踪中标记完成。

---

### Task 2: 1a — 死文件删除 + .gitignore 重写 + 运行时产物出仓

**Files:**
- Delete: `1.md`、`engine/drone.py.bak2`、`fix_drone_resource_leak.py`、`fix_drone_v2.py`、`security-review.ts`、`notepad_surface.json`、`go_vs_python_comparison.md`、`docs/python_vs_go_comparison.md`、`web_ui/index.html`、`requirements-worker.txt`、`bin/osv-scanner.exe`
- Modify: `.gitignore`（全文重写）
- Untrack: `__pycache__/`、`.blackboard.json`、`.attack_blackboard.json`、`.audit_notes.md`、`local_workspace/`、`tmp/`、`.workbuddy/`、`.cloudstudio`

**Interfaces:**
- Produces: 无死文件的仓库 + 防回归的 `.gitignore`

- [ ] **Step 1: 删除前最终验证零引用（防御性复查）**

```bash
cd /home/fuzz/huntzero
for f in "fix_drone_resource_leak" "fix_drone_v2" "security-review" "notepad_surface" "drone.py.bak2" "requirements-worker"; do
  echo "== $f =="; grep -rn "$f" --include="*.py" --include="*.md" --include="*.yml" --include="*.toml" . | grep -v "^./$f" | grep -v "^\./1\.md" | grep -v "^\./\.workbuddy" | grep -v "specs/" | grep -v "plans/" || echo "  (无引用)"
done
```

预期：每个文件除自身、`1.md`（运行日志，将删）、`.workbuddy/`（将 untrack）、docs/superpowers 外无引用。若发现真实引用 → 停止，回退该文件的删除并记录。

- [ ] **Step 2: 删除文件并 untrack 运行时产物**

```bash
cd /home/fuzz/huntzero
git rm -q 1.md engine/drone.py.bak2 fix_drone_resource_leak.py fix_drone_v2.py \
  security-review.ts notepad_surface.json go_vs_python_comparison.md \
  docs/python_vs_go_comparison.md web_ui/index.html requirements-worker.txt \
  bin/osv-scanner.exe
git rm -r -q --cached __pycache__ engine/__pycache__ tools/__pycache__ \
  .blackboard.json .attack_blackboard.json .audit_notes.md \
  local_workspace tmp .workbuddy .cloudstudio 2>/dev/null || true
```

- [ ] **Step 3: 重写 `.gitignore`（全文替换）**

```gitignore
# 环境与密钥
.env
.env.local
.env.*.local
*.secret
*.key
*.pem

# Python
__pycache__/
*.pyc
*.pyo
*.pyd
*.so
.Python
venv/
.venv/
*.egg-info/
dist/
build/

# 运行时产物（引擎）
.blackboard*.json
.audit_notes.md
local_workspace/
attack_workspace/
tmp/
reports/
*.vault

# 工具链本地产物
bin/osv-scanner*
.workbuddy/
.cloudstudio

# 日志
*.log
logs/

# IDE / OS
.idea/
.vscode/
*.swp
.DS_Store
```

- [ ] **Step 4: 验证仓库状态干净且引擎可导入**

```bash
cd /home/fuzz/huntzero
git status --short
python -c "import engine.cerebrum, engine.drone, server.core, kimi_hive; print('imports OK')"
python -m pytest tests/ -q
```

预期：`git status` 只剩 `.gitignore` 修改与删除暂存；imports OK（`.blackboard.json` 缺失不影响，Blackboard 会重建）；pytest 全绿。

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "chore: remove dead files and untrack runtime artifacts (SPEC 5.1)"
```

---

### Task 3: 1a — 物料移出产品仓（frontend/artifacts/PPT 文档/运营脚本/vendored 插件）

**Files:**
- Delete（先存档到旧仓）: `frontend/`、`artifacts/`、`docs/pages.json`、`docs/project.json`、`docs/page-global-config.json`、`docs/product/`、《当我们用AI挖到第一个高危——假设驱动认知审计实战.md》、`vuln-hunting-plugin/`、`sync_intel.py`、`sync_wildfire.py`
- Move: `docs/weaponization_flowchart_optimized.md`、`docs/weaponization_flowchart_text.md` → `docs/design/`

**Interfaces:**
- Produces: 产品仓只含产品与产品文档；营销/运营物料归档在旧仓 `/home/fuzz/kimiSec/_archive/from-zerodayFind/`

- [ ] **Step 1: 存档到旧仓**

```bash
mkdir -p /home/fuzz/kimiSec/_archive/from-zerodayFind
cd /home/fuzz/huntzero
cp -r frontend artifacts vuln-hunting-plugin sync_intel.py sync_wildfire.py \
  "当我们用AI挖到第一个高危——假设驱动认知审计实战.md" \
  docs/pages.json docs/project.json docs/page-global-config.json docs/product \
  /home/fuzz/kimiSec/_archive/from-zerodayFind/
ls /home/fuzz/kimiSec/_archive/from-zerodayFind/ | wc -l   # 预期 10
```

- [ ] **Step 2: 从产品仓删除并移动 weaponization 文档**

```bash
cd /home/fuzz/huntzero
git rm -r -q frontend artifacts vuln-hunting-plugin sync_intel.py sync_wildfire.py \
  "当我们用AI挖到第一个高危——假设驱动认知审计实战.md" \
  docs/pages.json docs/project.json docs/page-global-config.json docs/product
mkdir -p docs/design
git mv docs/weaponization_flowchart_optimized.md docs/design/
git mv docs/weaponization_flowchart_text.md docs/design/
```

- [ ] **Step 3: 检查残留引用并验证**

```bash
cd /home/fuzz/huntzero
grep -rn "sync_intel\|sync_wildfire\|vuln-hunting-plugin\|frontend/" \
  --include="*.py" --include="*.yml" --include="*.toml" . \
  | grep -v "docs/superpowers" || echo "(无代码引用)"
python -m pytest tests/ -q
```

预期：无代码引用（README 中的引用属文档过时，Task 17 重写 README 时处理）；pytest 全绿。注意 `.dockerignore` 若含 `vuln-hunting-plugin/` 行，保留无害（Task 6 配置文件统一时再校）。

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "chore: move marketing/ops material out of product repo (SPEC 5.1)"
```

---

### Task 4: 1a — 审查后删除（skills/scheduler、skills/find-skills）

**Files:**
- Delete（条件性）: `skills/scheduler/`、`skills/find-skills/`

**Interfaces:**
- Consumes: 无
- Produces: 若引用检查通过，安全领域技能库只剩安全相关 skill

- [ ] **Step 1: 引用验证（决定删不删）**

```bash
cd /home/fuzz/huntzero
echo "== cerebrum 中的领域加载逻辑 =="
grep -n "scheduler\|find-skills\|find_skills" engine/cerebrum.py || echo "  cerebrum 无引用"
echo "== 路由表 =="
grep -n "scheduler\|find-skills\|find_skills" skills/security-expert/SKILL.md || echo "  路由表无引用"
echo "== 其他 skill 交叉引用 =="
grep -rn "scheduler\|find-skills" skills/ --include="*.md" | grep -v "^skills/scheduler/" | grep -v "^skills/find-skills/" || echo "  无交叉引用"
```

预期：三处均无引用。若任一处有引用 → 该 skill 保留，记录到任务备注。

- [ ] **Step 2: 删除并验证**

```bash
cd /home/fuzz/huntzero
git rm -r -q skills/scheduler skills/find-skills
python -c "import engine.cerebrum; print('cerebrum imports OK')"
python -m pytest tests/ -q
```

- [ ] **Step 3: Commit**

```bash
git add -A
git commit -m "chore: drop non-security skills (scheduler, find-skills) (SPEC 5.1)"
```

---

### Task 5: 1b — .env.example 重写 + env 一致性校验脚本

**Files:**
- Modify: `.env.example`（全文重写）
- Create: `tools/check_env_sync.py`
- Test: `tests/test_env_sync.py`

**Interfaces:**
- Produces: `check_env_sync() -> tuple[set[str], set[str]]`（返回"代码有 example 缺"、"example 有代码缺"两个集合），被 `tests/test_env_sync.py` 与后续 CI 使用

- [ ] **Step 1: 写失败的测试**

`tests/test_env_sync.py`：

```python
"""SPEC 5.2：.env.example 必须与代码 env 读取点一致（防再脱节）。"""

from pathlib import Path

from tools.check_env_sync import check_env_sync

REPO_ROOT = Path(__file__).resolve().parent.parent


def test_env_example_covers_all_code_reads():
    missing_in_example, ghosts_in_example = check_env_sync(REPO_ROOT)
    assert missing_in_example == set(), f".env.example 缺少: {missing_in_example}"
    assert ghosts_in_example == set(), f".env.example 幽灵变量: {ghosts_in_example}"
```

运行：`python -m pytest tests/test_env_sync.py -q`，预期 FAIL（`ModuleNotFoundError: tools.check_env_sync`）。

- [ ] **Step 2: 实现校验脚本**

`tools/check_env_sync.py`：

```python
"""校验 .env.example 与代码 env 读取点一致。

扫描规则：*.py 中 os.environ[...] / os.getenv(...) / os.environ.get(...) 的
字面量变量名；排除测试、虚拟环境与本脚本自身。豁免名单列出"SDK 内部读取
但不应出现在 .env.example"的变量（由 kimi-agent-sdk 管理，非本项目配置面）。
"""

from __future__ import annotations

import re
from pathlib import Path

_ENV_READ = re.compile(
    r"""os\.environ(?:\.get)?\(\s*["']([A-Z][A-Z0-9_]+)["']"""
    r"""|os\.getenv\(\s*["']([A-Z][A-Z0-9_]+)["']"""
    r"""|os\.environ\[\s*["']([A-Z][A-Z0-9_]+)["']\s*\]"""
)

_EXAMPLE_LINE = re.compile(r"^\s*(?:#\s*)?([A-Z][A-Z0-9_]+)\s*=", re.MULTILINE)

# SDK/第三方内部读取的变量，不属于本项目配置面
_EXEMPT = {"POC_TARGET", "POC_ID"}  # tools/poc_sandbox.py 注入子进程用
_SKIP_DIRS = {".git", ".venv", "venv", "__pycache__", "node_modules", "tests"}


def _code_env_vars(repo_root: Path) -> set[str]:
    found: set[str] = set()
    for py in repo_root.rglob("*.py"):
        if any(part in _SKIP_DIRS for part in py.parts):
            continue
        if py.name == "check_env_sync.py":
            continue
        text = py.read_text(encoding="utf-8", errors="ignore")
        for m in _ENV_READ.finditer(text):
            found.add(next(g for g in m.groups() if g))
    return found - _EXEMPT


def _example_vars(example_path: Path) -> set[str]:
    text = example_path.read_text(encoding="utf-8")
    return set(_EXAMPLE_LINE.findall(text))


def check_env_sync(repo_root: Path) -> tuple[set[str], set[str]]:
    code = _code_env_vars(repo_root)
    example = _example_vars(repo_root / ".env.example")
    return code - example, example - code


if __name__ == "__main__":
    import sys

    missing, ghosts = check_env_sync(Path(__file__).resolve().parent.parent)
    if missing or ghosts:
        print(f"missing in .env.example: {sorted(missing)}")
        print(f"ghosts in .env.example: {sorted(ghosts)}")
        sys.exit(1)
    print("env sync OK")
```

- [ ] **Step 3: 重写 `.env.example`（全文替换）**

```bash
# ─── huntzero 环境变量模板 ─────────────────────────────
# 复制为 .env 并填入真实值（仅开发/自部署需要；VM 交付由 license 注入）

# LLM 认证（开发模式直读；license 模式下由客户端自动注入，无需设置）
KIMI_API_KEY=sk-your-api-key-here

# LLM 端点（可选，默认 https://api.kimi.com/coding/v1）
# KIMI_BASE_URL=https://api.kimi.com/coding/v1

# Redis 地址（可选；设置后 serve/worker 进入 cluster 模式）
# KIMI_REDIS_URL=redis://localhost:6379/0

# HTTP API 认证（serve 模式必需；X-API-Key 头 / WS ?api_key=）
# KIMISEC_API_KEY=change-me

# Redis 后端审计落盘目录（可选，默认 /data/reports）
# KIMI_REPORTS_DIR=/data/reports

# Round 0 文档情报超时秒数（可选，默认 120，下限 15）
# KIMI_DOC_INTEL_TIMEOUT=120

# OSV-Scanner 路径（可选，留空自动探测 ./bin/osv-scanner → PATH）
# OSV_SCANNER_PATH=/usr/local/bin/osv-scanner
```

- [ ] **Step 4: 跑测试验证通过**

```bash
cd /home/fuzz/huntzero
python -m pytest tests/test_env_sync.py -v
python tools/check_env_sync.py
```

预期：PASS；脚本输出 `env sync OK`。若 `KIMI_DOC_INTEL_TIMEOUT` 等因读取写法（如 `os.environ.get("X", "120")` 变体）漏匹配，修正脚本正则直至一致，**不改业务代码**。

- [ ] **Step 5: Commit**

```bash
git add .env.example tools/check_env_sync.py tests/test_env_sync.py
git commit -m "feat(config): rewrite .env.example and add env sync checker (SPEC 5.2)"
```

---

### Task 6: 1b — pyproject.toml 重写 + 依赖收敛

**Files:**
- Modify: `pyproject.toml`（全文重写）
- Modify: `requirements.txt`（改为导出说明）
- Modify: `docker-compose.yml`、`Dockerfile`（清幽灵变量插值）

**Interfaces:**
- Produces: `pip install -e ".[dev]"` 可完整安装本项目（含 kimi-agent-sdk）；`huntzero` CLI entry 在 Task 15 改名后生效，本任务先用 `kimi = "kimi:main"`

- [ ] **Step 1: 重写 `pyproject.toml`（全文替换）**

```toml
[build-system]
requires = ["setuptools>=61.0", "wheel"]
build-backend = "setuptools.build_meta"

[project]
name = "huntzero"
version = "0.1.0"
description = "huntzero — LLM 主导的假设驱动漏洞挖掘引擎（私有化交付）"
readme = "README.md"
requires-python = ">=3.11"
license = {text = "Proprietary"}
authors = [{name = "huntzero Team"}]
keywords = ["security", "vulnerability", "llm", "audit"]
dependencies = [
    "kimi-agent-sdk==0.0.5",
    "rich>=13.0",
    "pyyaml>=6.0",
    "redis[asyncio]>=5.0",
    "fastapi>=0.115",
    "uvicorn[standard]>=0.30",
    "pydantic>=2.0",
    "tree-sitter>=0.23",
    "tree-sitter-python",
    "tree-sitter-javascript",
    "tree-sitter-typescript",
    "tree-sitter-c",
    "tree-sitter-go",
    "tree-sitter-rust",
    "tree-sitter-java",
]

[project.optional-dependencies]
dev = [
    "pytest>=7.0.0",
    "pytest-asyncio>=0.23.0",
    "fakeredis>=2.20",
    "black>=24.0.0",
    "ruff>=0.3.0",
    "mypy>=1.9.0",
]

[project.scripts]
huntzero = "kimi:main"

[tool.setuptools.packages.find]
where = ["."]
include = ["engine*", "server*", "tools*"]
exclude = ["tests*", "docs*", "skills*"]

[tool.setuptools]
py-modules = ["kimi", "kimi_hive", "kimi_sdk_compat"]

[tool.black]
line-length = 100
target-version = ['py311']

[tool.ruff]
line-length = 100
target-version = "py311"
select = ["E", "F", "W", "I", "N", "UP"]
ignore = ["E501"]

[tool.mypy]
python_version = "3.11"
strict = true
ignore_missing_imports = true

[tool.pytest.ini_options]
testpaths = ["tests"]
python_files = ["test_*.py"]
asyncio_mode = "auto"
```

- [ ] **Step 2: 收敛 `requirements.txt` 为导出说明（全文替换）**

```text
# 依赖唯一权威是 pyproject.toml（SPEC 5.2）。
# 本文件仅为兼容旧工作流的导出快照，更新方式：
#   pip install -e . && pip freeze --exclude-editable > requirements.txt
# 新增/修改依赖请改 pyproject.toml，不要直接编辑本文件。
kimi-agent-sdk==0.0.5
rich>=13.0
pyyaml>=6.0
redis[asyncio]>=5.0
fastapi>=0.115
uvicorn[standard]>=0.30
pydantic>=2.0
tree-sitter>=0.23
tree-sitter-python
tree-sitter-javascript
tree-sitter-typescript
tree-sitter-c
tree-sitter-go
tree-sitter-rust
tree-sitter-java
```

- [ ] **Step 3: 清理 `docker-compose.yml` / `Dockerfile` 幽灵变量**

操作：
- `docker-compose.yml`：删除 `KIMI_WORKERS` 插值（代码零读取，worker 并发来自 job payload）；`KIMI_WORK_DIR` 若仅用于拼 CMD 参数则保留并加注释说明，否则删除
- `Dockerfile`：确保 `pip install` 覆盖 `kimi-agent-sdk`（改为 `pip install kimi-agent-sdk==0.0.5` 或直接 `pip install -r requirements.txt` 现已含它）

先读两个文件再改（逐处最小改动，不重排无关内容）。验证：

```bash
grep -n "KIMI_WORKERS\|ZDLL_LLM\|INTEL_SECRET\|WECOM_WEBHOOK" docker-compose.yml Dockerfile || echo "(幽灵变量已清)"
grep -n "kimi-agent-sdk" Dockerfile requirements.txt
```

- [ ] **Step 4: 安装验证**

```bash
cd /home/fuzz/huntzero
pip install -e ".[dev]"
python -c "import kimi_agent_sdk, engine.cerebrum, server.core; print('deps OK')"
python -m pytest tests/ -q
```

预期：安装成功（kimi-agent-sdk 0.0.5 已在 conda env 装过则秒过）；deps OK；pytest 全绿（含 test_env_sync）。

- [ ] **Step 5: Commit**

```bash
git add pyproject.toml requirements.txt docker-compose.yml Dockerfile
git commit -m "build: rewrite pyproject for huntzero and unify dependency declarations (SPEC 5.2)"
```

---

### Task 7: 1b — 合并重复的 build_config 到 engine/llm_config.py

**Files:**
- Create: `engine/llm_config.py`
- Modify: `kimi_hive.py:678-693`（替换为调用）、`server/core.py:363-381`（替换为调用）
- Test: `tests/test_llm_config.py`

**Interfaces:**
- Produces: `build_llm_config(api_key: str) -> "Config"`（返回 kimi-agent-sdk 的 Config；base_url 读 `KIMI_BASE_URL` 默认 `https://api.kimi.com/coding/v1`，model 读 `KIMI_MODEL_NAME` 默认 `kimi-for-coding`，max_context_size=262144，timeout=3600.0）。`kimi_hive.py` 与 `server/core.py` 都调用它

注意：这是 SPEC §5.2 明确授权的**结构性去重**，行为必须逐字段等价。先写表征测试锁定两处现有输出，再做替换。

- [ ] **Step 1: 写表征测试（锁定现状输出）**

`tests/test_llm_config.py`：

```python
"""SPEC 5.2：build_config 合并的表征测试 —— 先钉住两处现有实现的等价输出。"""

import os


def test_build_llm_config_matches_kimi_hive_variant(monkeypatch):
    monkeypatch.delenv("KIMI_BASE_URL", raising=False)
    monkeypatch.delenv("KIMI_MODEL_NAME", raising=False)
    from engine.llm_config import build_llm_config
    from kimi_hive import build_config as hive_build_config

    new = build_llm_config("sk-test-key")
    old = hive_build_config("sk-test-key")
    assert new.providers == old.providers
    assert new.models == old.models
    assert new.default_model == old.default_model


def test_build_llm_config_matches_server_core_variant(monkeypatch):
    monkeypatch.delenv("KIMI_BASE_URL", raising=False)
    monkeypatch.delenv("KIMI_MODEL_NAME", raising=False)
    from engine.llm_config import build_llm_config
    from server.core import KimiSecCore

    new = build_llm_config("sk-test-key")
    old = KimiSecCore.build_config("sk-test-key")
    assert new.providers == old.providers
    assert new.models == old.models
    assert new.default_model == old.default_model


def test_build_llm_config_env_override(monkeypatch):
    monkeypatch.setenv("KIMI_BASE_URL", "https://gateway.example.com/v1")
    monkeypatch.setenv("KIMI_MODEL_NAME", "custom-model")
    from engine.llm_config import build_llm_config

    cfg = build_llm_config("sk-test-key")
    provider = next(iter(cfg.providers.values()))
    assert provider.base_url == "https://gateway.example.com/v1"
    model = next(iter(cfg.models.values()))
    assert model.model == "custom-model"
```

注意：若 `kimi_hive.py` 的 `build_config` 是模块级私有名（如 `_build_config`），import 相应调整；`server/core.py` 的可能是 `@staticmethod` 或实例方法，按实际形态调用（先读源码确认两处精确签名再定稿本测试）。

运行：`python -m pytest tests/test_llm_config.py -q`，预期 FAIL（`ModuleNotFoundError: engine.llm_config`）。

- [ ] **Step 2: 读两处现状实现，写 `engine/llm_config.py`**

先读 `kimi_hive.py:678-693` 与 `server/core.py:363-381`，把逐字段等价实现写入 `engine/llm_config.py`，形态：

```python
"""统一的 LLM Config 构建（SPEC 5.2：合并 kimi_hive 与 server.core 的重复实现）。

行为契约（与两处旧实现逐字段等价）：
- provider: type="kimi", base_url=os.environ["KIMI_BASE_URL"] 或默认, timeout=3600.0
- model: os.environ["KIMI_MODEL_NAME"] 或 "kimi-for-coding", max_context_size=262144
"""

from __future__ import annotations

import os

from kimi_agent_sdk import Config

DEFAULT_BASE_URL = "https://api.kimi.com/coding/v1"
DEFAULT_MODEL = "kimi-for-coding"


def build_llm_config(api_key: str) -> Config:
    """构建 kimi-agent-sdk Config（字段与旧 build_config 完全一致）。"""
    # 按两处旧实现的真实字段逐一复刻（读源码后定稿，不得增减字段）
    ...
```

实现时把旧实现的 provider/model 构造原样搬入（dict 或对象，以旧代码为准），仅把硬编码的 base_url/model 替换为 `os.environ.get(...)` 读取。

- [ ] **Step 3: 替换两处调用点**

`kimi_hive.py` 与 `server/core.py` 中：删除本地 `build_config` 函数体，改为 `from engine.llm_config import build_llm_config as build_config`（保持原名，调用点零改动）；`server/core.py` 若作为静态方法被 `KimiSecCore.build_config(...)` 调用，保留一个同名 `@staticmethod` 委托：

```python
@staticmethod
def build_config(api_key: str):
    from engine.llm_config import build_llm_config

    return build_llm_config(api_key)
```

- [ ] **Step 4: 跑测试验证等价**

```bash
python -m pytest tests/test_llm_config.py tests/test_env_sync.py -v
python -c "import kimi_hive, server.core; print('imports OK')"
```

预期：三个测试全 PASS（等价性成立）。

- [ ] **Step 5: Commit**

```bash
git add engine/llm_config.py kimi_hive.py server/core.py tests/test_llm_config.py
git commit -m "refactor(config): merge duplicated build_config into engine/llm_config (SPEC 5.2)"
```

---

### Task 8: 1c — blackboard 单测（五层去重 + 合并语义 + 仲裁 + 持久化）

**Files:**
- Test: `tests/test_blackboard_dedup.py`、`tests/test_blackboard_core.py`

**Interfaces:**
- Consumes: `engine/blackboard.py` 的公开/私有 API（签名见下，已核实）
- Produces: 去重与核心语义的回归保护网

关键已核实事实（写断言依据）：
- `Blackboard(work_dir: Path)` 自动建目录；`add_hypothesis(description, confidence, parent_id=None) -> Optional[str]`；negative polarity 返回 None；重复时返回已有 h_id 并合并（置信度 `min(1.0, max(old,new)+merge_boost)`、`merge_boost=min(0.05, 0.02*[merged]证据数)`、追加 `"[merged] ..."` 证据上限 10 条）
- 去重判定 `_find_similar_hypothesis(description, threshold=0.55) -> Optional[str]`（同步私有方法，可直接测，对齐 test_sector.py 惯例）；Layer 0 锚点交集 + unigram ≥ 0.30；Layer 1 指纹全等；Layer 2 bigram ≥ 0.55；Layer 3 unigram ≥ 0.65；Layer 4 字符三元组 ≥ 0.70（两侧集合均 ≥5）；DISCARDED 不参与去重
- `classify_hypothesis_polarity(description) -> "positive"|"negative"`（模块级函数）
- `add_task(hypothesis_id, description, drone_role="general") -> Optional[str]`：同 hypothesis+role 下 `_simplify` 后包含且长度差 ≤10 → None
- `add_finding(hypothesis_id, title, description, severity, evidence, sector_id=None, **kwargs) -> str`：同 hypothesis_id 或 title token Jaccard ≥ 0.6 → 合并（severity 升级映射 critical4/high3/medium2/low1/none0，evidence 追加 `[corroborated]`），返回已有 f_id
- `request_arbitration(question, context) -> bool`：`await` 阻塞直到消费者 `item["future"].set_result(...)`
- `snapshot()` 顶层键 `target/active/hypotheses/tasks/findings`；`load()` 原子恢复（损坏 JSON 不动现状，且不恢复 `active`）

- [ ] **Step 1: 写去重测试文件**

`tests/test_blackboard_dedup.py`：

```python
"""engine/blackboard.py 五层去重管线与合并语义（SPEC 5.3）。

分层用例通过同步私有方法 _find_similar_hypothesis 直接验证（对齐
tests/test_sector.py 直测私有方法的惯例）；合并语义走 add_hypothesis。
注意：用例 wording 留足阈值余量，避免停用词表边缘效应。
"""

from pathlib import Path

import pytest

from engine.blackboard import Blackboard, classify_hypothesis_polarity


@pytest.fixture
def bb(tmp_path: Path) -> Blackboard:
    return Blackboard(work_dir=tmp_path)


# ───────────── 极性 ─────────────


class TestPolarity:
    def test_negative_hypothesis_rejected(self, bb):
        assert classify_hypothesis_polarity("memcpy is not vulnerable to overflow") == "negative"

    def test_positive_by_default(self, bb):
        assert classify_hypothesis_polarity("") == "positive"
        assert classify_hypothesis_polarity("buffer overflow in parse_header") == "positive"

    async def test_add_negative_returns_none(self, bb):
        h_id = await bb.add_hypothesis("This is not exploitable and poses no risk", 0.7)
        assert h_id is None
        assert len(bb.hypotheses) == 0


# ───────────── Layer 0 锚点匹配 ─────────────


class TestLayer0Anchor:
    async def test_shared_function_anchor_with_moderate_overlap(self, bb):
        base = "TIFFReadDirEntryArray in tif_dirread.c allocates count typesize bytes without overflow check"
        variant = "TIFFReadDirEntryArray allocates array without validating count"
        first = await bb.add_hypothesis(base, 0.6)
        second = await bb.add_hypothesis(variant, 0.7)
        assert first is not None
        assert second == first  # 锚点交集 + unigram≥0.30 → 合并


# ───────────── Layer 1 结构指纹 ─────────────


class TestLayer1Fingerprint:
    async def test_identical_description_merges(self, bb):
        desc = "stack buffer overflow in parse_config when copying token into fixed buffer"
        first = await bb.add_hypothesis(desc, 0.5)
        second = await bb.add_hypothesis(desc, 0.6)
        assert second == first
        assert len(bb.hypotheses) == 1


# ───────────── 合并语义 ─────────────


class TestMergeSemantics:
    async def test_confidence_boost_and_merged_evidence(self, bb):
        desc = "integer underflow in gxclread.c band read process leads to huge memcpy"
        first = await bb.add_hypothesis(desc, 0.5)
        await bb.add_hypothesis(desc, 0.6)
        node = bb.hypotheses[first]
        # 首次合并：已有 [merged] 证据数=0 → boost=0 → new = max(0.5, 0.6) = 0.6
        assert node.confidence == pytest.approx(0.6)
        assert any("[merged]" in e for e in node.evidence)

    async def test_distinct_hypotheses_not_merged(self, bb):
        a = await bb.add_hypothesis(
            "sql injection in login form through username parameter", 0.6
        )
        b = await bb.add_hypothesis(
            "race condition in filesystem journal replay during crash recovery", 0.6
        )
        assert a is not None and b is not None and a != b
        assert len(bb.hypotheses) == 2

    async def test_discarded_hypothesis_excluded_from_dedup(self, bb):
        from engine.blackboard import HypothesisStatus

        desc = "use after free in connection pool cleanup path"
        first = await bb.add_hypothesis(desc, 0.5)
        await bb.update_hypothesis(first, status=HypothesisStatus.DISCARDED)
        second = await bb.add_hypothesis(desc, 0.5)
        assert second != first
        assert len(bb.hypotheses) == 2
```

（Layer 2/3/4 的独立触发用例对停用词表敏感，执行时若某个分层用例未按预期合并，先用 `_find_similar_hypothesis` 直接调试该层，再以同样思路加用例；合并/不合并的行为断言必须全部通过。）

- [ ] **Step 2: 跑测试验证**

```bash
python -m pytest tests/test_blackboard_dedup.py -v
```

预期：全 PASS。若 `test_confidence_boost_and_merged_evidence` 的置信度精确值与实现有出入，以 `>= 0.6` 分支断言为准（保留两行断言中的宽松行，删除精确行）。

- [ ] **Step 3: 写核心语义测试文件**

`tests/test_blackboard_core.py`：

```python
"""blackboard 任务/发现/仲裁/持久化核心语义（SPEC 5.3）。"""

import asyncio
import json
from pathlib import Path

import pytest

from engine.blackboard import Blackboard


@pytest.fixture
def bb(tmp_path: Path) -> Blackboard:
    b = Blackboard(work_dir=tmp_path)
    b.target = "test-project"
    return b


# ───────────── 任务去重 ─────────────


class TestTaskDedup:
    async def test_contained_task_description_rejected(self, bb):
        h = await bb.add_hypothesis("overflow in parse_header when length unchecked", 0.6)
        t1 = await bb.add_task(h, "check parse_header bounds validation logic")
        t2 = await bb.add_task(h, "check parse_header bounds validation logic now")
        assert t1 is not None
        assert t2 is None  # 包含且长度差 ≤10 → 去重

    async def test_different_role_not_deduped(self, bb):
        h = await bb.add_hypothesis("overflow in parse_header when length unchecked", 0.6)
        t1 = await bb.add_task(h, "check parse_header bounds validation logic", drone_role="investigator")
        t2 = await bb.add_task(h, "check parse_header bounds validation logic", drone_role="exploit-crafter")
        assert t1 is not None and t2 is not None  # 去重仅限同 role


# ───────────── 发现合并 ─────────────


class TestFindingMerge:
    async def test_same_hypothesis_finding_merges_with_severity_upgrade(self, bb):
        h = await bb.add_hypothesis("overflow in parse_header when length unchecked", 0.6)
        f1 = await bb.add_finding(h, "parse_header overflow", "desc a", "medium", "evidence a")
        f2 = await bb.add_finding(h, "parse_header overflow different", "desc b", "high", "evidence b")
        assert f2 == f1
        assert len(bb.findings) == 1
        assert bb.findings[0].severity == "high"  # 升级
        assert "[corroborated]" in bb.findings[0].evidence

    async def test_similar_title_merges(self, bb):
        h1 = await bb.add_hypothesis("overflow in parser alpha when copying tokens", 0.6)
        h2 = await bb.add_hypothesis("underflow in writer beta during flush", 0.6)
        f1 = await bb.add_finding(h1, "buffer overflow in parse function", "d1", "high", "e1")
        f2 = await bb.add_finding(h2, "buffer overflow in parse routine", "d2", "medium", "e2")
        assert f2 == f1  # title token Jaccard ≥ 0.6 → 合并
        assert len(bb.findings) == 1


# ───────────── 仲裁队列 ─────────────


class TestArbitration:
    async def test_request_arbitration_blocks_until_answered(self, bb):
        task = asyncio.create_task(bb.request_arbitration("允许删除 tmp 吗？", "context"))
        item = await bb.arbitration_queue.get()
        assert item["question"] == "允许删除 tmp 吗？"
        assert not task.done()  # 阻塞中
        item["future"].set_result(True)
        assert await task is True


# ───────────── 持久化 ─────────────


class TestPersistence:
    async def test_snapshot_load_roundtrip(self, tmp_path):
        bb1 = Blackboard(work_dir=tmp_path)
        bb1.target = "proj-x"
        h = await bb1.add_hypothesis("overflow in parse_header when length unchecked", 0.6)
        await bb1.add_task(h, "verify bounds checks")
        await bb1.add_finding(h, "parse_header overflow", "desc", "high", "ev")

        bb2 = Blackboard(work_dir=tmp_path)
        bb2.load()
        assert bb2.target == "proj-x"
        assert h in bb2.hypotheses
        assert len(bb2.findings) == 1
        assert bb2.active is False  # load 不恢复 active

    async def test_load_corrupted_json_keeps_current_state(self, tmp_path):
        bb = Blackboard(work_dir=tmp_path)
        bb.target = "original"
        (tmp_path / ".blackboard.json").write_text("{corrupted", encoding="utf-8")
        bb.load()  # 不抛异常
        assert bb.target == "original"

    async def test_add_hypothesis_persists_file(self, bb, tmp_path):
        await bb.add_hypothesis("overflow in parse_header when length unchecked", 0.6)
        data = json.loads((tmp_path / ".blackboard.json").read_text(encoding="utf-8"))
        assert set(data.keys()) >= {"target", "active", "hypotheses", "tasks", "findings"}
        assert len(data["hypotheses"]) == 1
```

- [ ] **Step 4: 跑测试并修正（仅修测试不改引擎）**

```bash
python -m pytest tests/test_blackboard_core.py -v
```

预期：全 PASS。若某个去重阈值行为与断言不符，**改测试断言贴合实现**（钉住现状），并在 `docs/suspicious-registry.md` 登记该行为（Task 13 统一建册时可补录）。

- [ ] **Step 5: Commit**

```bash
git add tests/test_blackboard_dedup.py tests/test_blackboard_core.py
git commit -m "test(blackboard): add dedup pipeline and core semantics tests (SPEC 5.3)"
```

---

### Task 9: 1c — Bayesian 置信度引擎 + 存储后端单测

**Files:**
- Test: `tests/test_bayesian.py`、`tests/test_backends.py`

**Interfaces:**
- Consumes: `BayesianConfidenceEngine(hypotheses: dict, strength_path=None, strength_matrix=None)`；`LocalBackend`/`IncrementalLocalBackend`/`RedisBackend`/`make_backend(dsn, node_id, work_dir)`

关键已核实事实：
- Bayesian：`record_outcome(h_id, confirmed)`；`propagate_all()` 公式 `0.6*(parent_conf*strength)+0.4*own` clamp [0,1]；`strength` 默认 0.85（`n>=3` 才用学习值）；`update_confidences()` 原地改写返回 |Δ|>0.01 个数；`save()` 写 `{strength_path}/.bayesian_strength.json` 顶层键 `strength_matrix/meta`；无 parent 返回自身 confidence
- IncrementalLocalBackend：`COMPACT_INTERVAL=50`；MD5 相同跳过；非 compaction 时全量快照经 executor 异步写（断言前 `await asyncio.sleep(0.05)`）；WAL 每行 JSON 含 `ts/hash/findings_count/hypotheses_count/tasks_count`
- RedisBackend：同步 `redis.from_url`（monkeypatch 注入 fakeredis）；save 写 `kimisec:node:{id}:snapshot` + expire 7d + zadd `kimisec:global:findings` + hset `kimisec:global:cluster`
- `make_backend`：`redis://`→RedisBackend；`local-incremental://`→IncrementalLocalBackend；其他→LocalBackend（显式传 work_dir）

- [ ] **Step 1: 写 Bayesian 测试**

`tests/test_bayesian.py`：

```python
"""BayesianConfidenceEngine 置信度传播（SPEC 5.3）。"""

from pathlib import Path

import pytest

from engine.blackboard import BayesianConfidenceEngine, Blackboard


@pytest.fixture
def bb(tmp_path: Path) -> Blackboard:
    return Blackboard(work_dir=tmp_path)


class TestPropagation:
    async def test_no_parent_returns_own_confidence(self, bb):
        h = await bb.add_hypothesis("overflow in parser alpha when copying tokens", 0.7)
        engine = BayesianConfidenceEngine(bb.hypotheses)
        assert engine.propagate_all()[h] == pytest.approx(0.7)

    async def test_parent_chain_formula(self, bb):
        parent = await bb.add_hypothesis("parser alpha has unchecked length prefix", 0.8)
        child = await bb.add_hypothesis(
            "length prefix leads to heap overflow in token copy", 0.5, parent_id=parent
        )
        engine = BayesianConfidenceEngine(bb.hypotheses)
        result = engine.propagate_all()
        expected = min(1.0, max(0.0, 0.6 * (0.8 * 0.85) + 0.4 * 0.5))
        assert result[child] == pytest.approx(expected)

    async def test_update_confidences_rewrites_in_place(self, bb):
        parent = await bb.add_hypothesis("parser alpha has unchecked length prefix", 1.0)
        child = await bb.add_hypothesis(
            "length prefix leads to heap overflow in token copy", 0.0, parent_id=parent
        )
        engine = BayesianConfidenceEngine(bb.hypotheses)
        engine.update_confidences()
        assert bb.hypotheses[child].confidence > 0.0  # 原地改写

    async def test_cycle_safe(self, bb):
        a = await bb.add_hypothesis("hypothesis alpha about parser", 0.5)
        b = await bb.add_hypothesis("hypothesis beta about writer", 0.5)
        await bb.update_hypothesis(a, parent_id=b)
        await bb.update_hypothesis(b, parent_id=a)
        engine = BayesianConfidenceEngine(bb.hypotheses)
        result = engine.propagate_all()  # 不挂死
        assert set(result) == {a, b}


class TestStrengthLearning:
    async def test_learned_strength_requires_min_samples(self, bb, tmp_path):
        engine = BayesianConfidenceEngine(bb.hypotheses, strength_path=tmp_path)
        parent = await bb.add_hypothesis("parser alpha has unchecked length prefix", 0.8)
        for _ in range(3):
            child = await bb.add_hypothesis(
                f"variant finding about length prefix {len(bb.hypotheses)}", 0.5,
                parent_id=parent,
            )
            engine.record_outcome(child, confirmed=True)
        stats = engine.get_strength_stats()
        assert stats["learned_pairs"] >= 1
        assert engine.save() is True
        assert (tmp_path / ".bayesian_strength.json").exists()

    def test_save_without_path_returns_false(self, bb):
        assert BayesianConfidenceEngine(bb.hypotheses).save() is False
```

- [ ] **Step 2: 写后端测试**

`tests/test_backends.py`：

```python
"""engine/backends.py 存储后端（SPEC 5.3）。"""

import asyncio
import json
from pathlib import Path

import pytest

from engine.backends import (
    IncrementalLocalBackend,
    LocalBackend,
    RedisBackend,
    make_backend,
)


def _snapshot(findings: int = 1) -> dict:
    return {
        "target": "proj",
        "active": True,
        "hypotheses": {"H-1": {"id": "H-1", "confidence": 0.6}},
        "tasks": {},
        "findings": [
            {"id": f"F-{i}", "title": f"t{i}", "severity": "high", "created_at": float(i)}
            for i in range(findings)
        ],
    }


# ───────────── LocalBackend ─────────────


class TestLocalBackend:
    def test_atomic_save_and_load(self, tmp_path):
        be = LocalBackend(tmp_path)
        be.save("n1", _snapshot())
        assert (tmp_path / ".blackboard.json").exists()
        assert not (tmp_path / ".blackboard.json.tmp").exists()  # rename 完成
        loaded = be.load("n1")
        assert loaded["target"] == "proj"

    def test_load_missing_returns_none(self, tmp_path):
        assert LocalBackend(tmp_path).load("nope") is None

    def test_load_corrupted_returns_none(self, tmp_path):
        (tmp_path / ".blackboard.json").write_text("{bad", encoding="utf-8")
        assert LocalBackend(tmp_path).load("n1") is None


# ───────────── IncrementalLocalBackend ─────────────


class TestIncrementalLocalBackend:
    async def test_md5_dedup_skips_identical_save(self, tmp_path):
        be = IncrementalLocalBackend(tmp_path)
        be.save("n1", _snapshot())
        assert be._write_count == 1
        be.save("n1", _snapshot())  # 内容相同
        assert be._write_count == 1  # 被 MD5 跳过

    async def test_wal_append_and_compaction(self, tmp_path):
        be = IncrementalLocalBackend(tmp_path)
        for i in range(50):
            be.save("n1", _snapshot(findings=i + 1))
            await asyncio.sleep(0.02)  # 让 executor 轮转
        assert be._write_count == 50
        # 第 50 次触发 compaction：WAL 被删除、全量文件存在且为最新
        assert not (tmp_path / ".blackboard.wal").exists()
        data = json.loads((tmp_path / ".blackboard.json").read_text(encoding="utf-8"))
        assert len(data["findings"]) == 50


# ───────────── RedisBackend（fakeredis）─────────────


class TestRedisBackend:
    def test_save_writes_snapshot_findings_heartbeat(self, monkeypatch):
        import fakeredis
        import redis

        fake = fakeredis.FakeRedis(decode_responses=True)
        monkeypatch.setattr(redis, "from_url", lambda *a, **k: fake)
        be = RedisBackend("redis://fake:6379/0", node_id="node-a")
        be.save("node-a", _snapshot())
        assert fake.get("kimisec:node:node-a:snapshot") is not None
        assert fake.ttl("kimisec:node:node-a:snapshot") > 0
        assert fake.zcard("kimisec:global:findings") == 1
        assert fake.hexists("kimisec:global:cluster", "node-a:heartbeat")


# ───────────── make_backend ─────────────


class TestMakeBackend:
    def test_dispatch(self, monkeypatch, tmp_path):
        import fakeredis
        import redis

        monkeypatch.setattr(redis, "from_url", lambda *a, **k: fakeredis.FakeRedis(decode_responses=True))
        assert isinstance(make_backend("redis://x:6379/0", work_dir=tmp_path), RedisBackend)
        assert isinstance(
            make_backend("local-incremental://", work_dir=tmp_path), IncrementalLocalBackend
        )
        assert isinstance(make_backend("local://", work_dir=tmp_path), LocalBackend)
```

- [ ] **Step 3: 跑测试并修正（仅修测试）**

```bash
pip install fakeredis
python -m pytest tests/test_bayesian.py tests/test_backends.py -v
```

预期：全 PASS。`test_learned_strength_requires_min_samples` 若因子假设描述互相去重导致 child 数不足，增大描述差异（改数字后缀即可，已内置）。WAL/compaction 时序若偶发失败，把 `sleep(0.02)` 调到 `0.05`。

- [ ] **Step 4: Commit**

```bash
git add tests/test_bayesian.py tests/test_backends.py
git commit -m "test(engine): add bayesian engine and storage backend tests (SPEC 5.3)"
```

---

### Task 10: 1c — cerebrum 解析与 TerminationGuard 单测

**Files:**
- Test: `tests/test_cerebrum_parsing.py`、`tests/test_termination_guard.py`

**Interfaces:**
- Consumes: `Cerebrum(blackboard, work_dir, root_dir, config, ...)`（`config=None` 在纯解析测试中安全）；`_parse_json_output(data: dict) -> bool`；`_parse_output(text: str) -> bool`；`TerminationGuard(max_rounds, max_tasks, max_wall_time, stagnation_rounds)` 与 `check(round_num, blackboard, active_drones, total_tasks) -> tuple[bool, str]`

关键已核实行为（断言依据）：
- hypotheses：`conf <= 0.15` 跳过；negative polarity 跳过；confidence 钳制 [0,1] 失败默认 0.5
- tasks：`description` 空跳过；解析器侧 `_total_tasks` +1（dispatcher 侧还会 +1，**双重计数是现状**，进登记册）
- findings：`title` 非空且 `confidence >= 0.70` 硬门槛；severity 非 `critical|high|medium|low` 归一为 medium；无匹配假设时创建 `[Cerebrum-direct]` 假设承载
- GC：活跃假设 > 15 时按 confidence 降序保留 15；`_auto_promote_high_confidence_hypotheses`：conf ≥ 0.90 自动补 Finding
- `is_complete=True` → 返回 True 且 `_running=False`；`phase_complete=True`（is_complete 假）→ 返回 True 不动 `_running`
- `_parse_output`：空串 False；` ```json ` 围栏剥除（仅当文本以围栏开头）；前置杂文本 + 首个配平 JSON 对象可解析；截断 JSON 落 XML 兜底不抛异常
- TerminationGuard 判定序：L4a `round_num >= max_rounds` → `"budget:max_rounds=N"`；L4b `total_tasks >= max_tasks`；L2 收敛（hypotheses>0 且全部 terminal 且无活跃任务无活跃 drone）→ `"converged:all_N_hypotheses_terminal"`；L3 停滞需按序调用累计

- [ ] **Step 1: 写解析测试**

`tests/test_cerebrum_parsing.py`：

```python
"""engine/cerebrum.py 输出解析（SPEC 5.3）。直调 _parse_json_output/_parse_output。"""

from pathlib import Path

import pytest

from engine.blackboard import Blackboard, HypothesisStatus
from engine.cerebrum import Cerebrum


@pytest.fixture
def cerebrum(tmp_path: Path) -> Cerebrum:
    bb = Blackboard(work_dir=tmp_path)
    return Cerebrum(blackboard=bb, work_dir=tmp_path, root_dir=tmp_path, config=None)


# ───────────── hypotheses 过滤 ─────────────


class TestHypothesisParsing:
    async def test_valid_hypothesis_added(self, cerebrum):
        done = await cerebrum._parse_json_output(
            {"hypotheses": [{"claim": "overflow in parse_header when length unchecked", "confidence": 0.8}]}
        )
        assert done is False
        assert len(cerebrum.blackboard.hypotheses) == 1
        node = next(iter(cerebrum.blackboard.hypotheses.values()))
        assert node.confidence == pytest.approx(0.8)

    async def test_low_confidence_skipped(self, cerebrum):
        await cerebrum._parse_json_output(
            {"hypotheses": [{"claim": "maybe something sketchy in parser", "confidence": 0.15}]}
        )
        assert len(cerebrum.blackboard.hypotheses) == 0  # conf <= 0.15 跳过

    async def test_negative_polarity_skipped(self, cerebrum):
        await cerebrum._parse_json_output(
            {"hypotheses": [{"claim": "This is not exploitable and poses no risk", "confidence": 0.9}]}
        )
        assert len(cerebrum.blackboard.hypotheses) == 0

    async def test_confidence_clamped(self, cerebrum):
        await cerebrum._parse_json_output(
            {"hypotheses": [{"claim": "overflow in parse_header when length unchecked", "confidence": 3.7}]}
        )
        node = next(iter(cerebrum.blackboard.hypotheses.values()))
        assert node.confidence == pytest.approx(1.0)


# ───────────── tasks 入队 ─────────────


class TestTaskParsing:
    async def test_task_queued_and_counted(self, cerebrum):
        await cerebrum._parse_json_output(
            {
                "hypotheses": [{"claim": "overflow in parse_header when length unchecked", "confidence": 0.8}],
                "tasks": [
                    {
                        "hypothesis_ref": "overflow in parse_header",
                        "role": "investigator",
                        "description": "verify bounds check in parse_header",
                    }
                ],
            }
        )
        assert cerebrum._total_tasks == 1  # 解析器侧 +1；dispatcher 另有 +1（登记册条目）
        assert cerebrum._task_queue.qsize() == 1

    async def test_empty_description_skipped(self, cerebrum):
        await cerebrum._parse_json_output(
            {
                "hypotheses": [{"claim": "overflow in parse_header when length unchecked", "confidence": 0.8}],
                "tasks": [{"hypothesis_ref": "overflow", "role": "investigator", "description": ""}],
            }
        )
        assert cerebrum._task_queue.qsize() == 0


# ───────────── findings 硬门槛 ─────────────


class TestFindingParsing:
    async def test_low_confidence_finding_rejected(self, cerebrum):
        await cerebrum._parse_json_output(
            {"findings": [{"title": "parse_header overflow", "description": "d", "severity": "high", "evidence": "e", "confidence": 0.5}]}
        )
        assert len(cerebrum.blackboard.findings) == 0  # < 0.70 硬门槛

    async def test_finding_accepted_and_severity_normalized(self, cerebrum):
        await cerebrum._parse_json_output(
            {"findings": [{"title": "parse_header overflow", "description": "d", "severity": "bogus", "evidence": "e", "confidence": 0.9}]}
        )
        assert len(cerebrum.blackboard.findings) == 1
        assert cerebrum.blackboard.findings[0].severity == "medium"  # 非白名单归一

    async def test_gc_and_auto_promote(self, cerebrum):
        # 直接播种 20 个假设（绕过 add_hypothesis 去重，确定性隔离测试 GC 逻辑）
        from engine.blackboard import HypothesisNode, HypothesisStatus

        for i in range(20):
            cerebrum.blackboard.hypotheses[f"H-{i:03d}"] = HypothesisNode(
                id=f"H-{i:03d}",
                description=f"seeded hypothesis {i}",
                confidence=0.3 + i * 0.01,
                status=HypothesisStatus.PENDING,
                tasks=[],
                evidence=[],
            )
        await cerebrum._parse_json_output(
            {
                "hypotheses": [
                    {"claim": "critical overflow in parse_header with code ref at parser.c:42", "confidence": 0.95}
                ]
            }
        )
        active = [
            h
            for h in cerebrum.blackboard.hypotheses.values()
            if h.status not in (HypothesisStatus.DISCARDED, HypothesisStatus.CONFIRMED)
        ]
        assert len(active) <= 15  # GC 按 confidence 降序保留 top-15
        assert len(cerebrum.blackboard.findings) >= 1  # conf≥0.90 自动晋升 Finding


# ───────────── 完成信号 ─────────────


class TestCompletionSignals:
    async def test_is_complete_stops_running(self, cerebrum):
        cerebrum._running = True
        done = await cerebrum._parse_json_output({"is_complete": True, "complete_reason": "audit done"})
        assert done is True
        assert cerebrum._running is False

    async def test_phase_complete_keeps_running(self, cerebrum):
        cerebrum._running = True
        done = await cerebrum._parse_json_output({"phase_complete": True})
        assert done is True
        assert cerebrum._running is True  # 阶段推进不置停

    async def test_parse_output_empty_and_fenced(self, cerebrum):
        assert await cerebrum._parse_output("") is False
        assert await cerebrum._parse_output("   \n  ") is False
        fenced = '```json\n{"is_complete": true, "complete_reason": "done"}\n```'
        cerebrum._running = True
        assert await cerebrum._parse_output(fenced) is True

    async def test_parse_output_junk_prefix_and_truncated(self, cerebrum):
        cerebrum._running = True
        junk = '一些前置思考文本 {"is_complete": true, "complete_reason": "x"} 后续文本'
        assert await cerebrum._parse_output(junk) is True  # 首个配平对象可解析
        # 截断 JSON：不抛异常，落 XML 兜底
        cerebrum._running = True
        result = await cerebrum._parse_output('{"hypotheses": [{"claim": "abc')
        assert isinstance(result, bool)
```

- [ ] **Step 2: 跑解析测试**

```bash
cd /home/fuzz/huntzero && python -m pytest tests/test_cerebrum_parsing.py -v
```

预期：全 PASS。若 `test_gc_and_auto_promote` 的 GC 边界与实现有出入（如保留数含 CONFIRMED），以实际行为为准修正断言并备注。

- [ ] **Step 3: 写 TerminationGuard 测试**

`tests/test_termination_guard.py`：

```python
"""TerminationGuard 五层终止判定（SPEC 5.3）。L1 在解析器、L5 在外部 wait_for，不在本类。"""

from pathlib import Path

import pytest

from engine.blackboard import Blackboard, HypothesisStatus
from engine.cerebrum import TerminationGuard


@pytest.fixture
def bb(tmp_path: Path) -> Blackboard:
    return Blackboard(work_dir=tmp_path)


class TestBudgetLayers:
    def test_max_rounds(self, bb):
        g = TerminationGuard(max_rounds=3, max_tasks=100, max_wall_time=99999)
        stop, reason = g.check(3, bb, active_drones=1, total_tasks=0)
        assert stop is True and reason == "budget:max_rounds=3"

    def test_max_tasks(self, bb):
        g = TerminationGuard(max_rounds=30, max_tasks=5, max_wall_time=99999)
        stop, reason = g.check(0, bb, active_drones=1, total_tasks=5)
        assert stop is True and reason == "budget:max_tasks=5"

    def test_continue_when_nothing_hit(self, bb):
        g = TerminationGuard(max_rounds=30, max_tasks=100, max_wall_time=99999)
        stop, reason = g.check(1, bb, active_drones=1, total_tasks=0)
        assert stop is False and reason == ""


class TestConvergence:
    async def test_all_terminal_converges(self, bb):
        h = await bb.add_hypothesis("overflow in parse_header when length unchecked", 0.8)
        await bb.update_hypothesis(h, status=HypothesisStatus.CONFIRMED)
        g = TerminationGuard(max_rounds=30, max_tasks=100, max_wall_time=99999)
        stop, reason = g.check(1, bb, active_drones=0, total_tasks=0)
        assert stop is True and reason == "converged:all_1_hypotheses_terminal"

    async def test_zero_hypotheses_never_converges(self, bb):
        g = TerminationGuard(max_rounds=30, max_tasks=100, max_wall_time=99999)
        stop, _ = g.check(1, bb, active_drones=0, total_tasks=0)
        assert stop is False  # L2 要求 hypotheses > 0


class TestStagnation:
    async def test_stagnation_accumulates_then_fires(self, bb):
        # findings>0 且假设保持 PENDING（不触发 L2 收敛）：用不存在的 hypothesis_id 加 finding
        await bb.add_finding("H-nonexistent", "t", "d", "high", "e")
        g = TerminationGuard(max_rounds=30, max_tasks=100, max_wall_time=99999, stagnation_rounds=2)
        s1, _ = g.check(1, bb, active_drones=0, total_tasks=0)  # 计数变化，归零
        s2, _ = g.check(2, bb, active_drones=0, total_tasks=0)  # 持平 ×1
        s3, reason = g.check(3, bb, active_drones=0, total_tasks=0)  # 持平 ×2 → 触发
        assert (s1, s2) == (False, False)
        assert s3 is True and reason.startswith("stagnation:no_new_findings_for_2_rounds")

    async def test_active_drones_pause_stagnation(self, bb):
        await bb.add_finding("H-nonexistent", "t", "d", "high", "e")
        g = TerminationGuard(max_rounds=30, max_tasks=100, max_wall_time=99999, stagnation_rounds=1)
        g.check(1, bb, active_drones=0, total_tasks=0)
        stop, _ = g.check(2, bb, active_drones=1, total_tasks=0)  # 有活跃 drone → 不计停滞
        assert stop is False
```

- [ ] **Step 4: 跑测试**

```bash
python -m pytest tests/test_termination_guard.py -v
```

预期：全 PASS。`test_active_drones_pause_stagnation` 若实现是"有 drone 时归零计数"而非"暂停"，断言同样成立（stop=False）；若断言不成立，以实际行为修正并备注。

- [ ] **Step 5: Commit**

```bash
git add tests/test_cerebrum_parsing.py tests/test_termination_guard.py
git commit -m "test(cerebrum): add output parsing and termination guard tests (SPEC 5.3)"
```

---

### Task 11: 1c — sector / osv_bridge / exploit_analyzer 单测

**Files:**
- Test: `tests/test_sector_heuristics.py`、`tests/test_osv_bridge.py`、`tests/test_exploit_analyzer.py`

**Interfaces:**
- Consumes: `SectorManager(project_root, blackboard, config)` 的 `_heuristic_decompose(sorted_dirs)` / `_parse_sector_output(raw)`；`osv_bridge._parse_osv_json(data, elapsed)`、`_normalize_severity`、`_cvss_to_severity`、`osv_findings_to_blackboard(result)`、`build_cerebrum_context(result)`、`scan_dependencies(target)`；`tools.exploit_analyzer._static_analysis(desc, code)`、`analyze_finding(finding, code_context, use_llm, target_env)`

- [ ] **Step 1: 写 sector 启发式测试**

`tests/test_sector_heuristics.py`：

```python
"""engine/sector.py 启发式回退分区与 LLM 输出解析（SPEC 5.3）。

对齐 tests/test_sector.py 直测私有方法的惯例。
注意：动态预算代码内联在 cerebrum.py:3116 且存在 getattr(sector,'file_count',0)
恒为 0 的缺陷（Sector 只有 estimated_files）——入登记册，不在本文件测。
"""

from pathlib import Path

import pytest

from engine.blackboard import Blackboard
from engine.sector import SectorManager


@pytest.fixture
def sm() -> SectorManager:
    return SectorManager(project_root=Path("."), blackboard=Blackboard(work_dir=Path(".")), config=None)


class TestHeuristicDecompose:
    def test_skip_and_priority_and_limit(self, sm):
        dirs = [
            ("docs", 300),      # skip_names 精确匹配 → 跳过
            ("auth", 200),      # P0（auth 命中）
            ("network", 150),   # P0（子串 "net" 先命中）
            ("utils", 30),      # P3
            ("tiny", 10),       # < 25 → 跳过
        ]
        sectors = sm._heuristic_decompose(dirs)
        names = [s.name for s in sectors]
        assert "Docs" not in names and "Tiny" not in names
        assert [s.id for s in sectors] == ["sector-auth", "sector-network", "sector-utils"]
        assert [s.priority for s in sectors] == [0, 0, 3]  # 按 (priority, -files) 排序
        assert sectors[0].estimated_files == 200

    def test_max_eight_sectors(self, sm):
        dirs = [(f"mod{i}", 100 - i) for i in range(12)]
        assert len(sm._heuristic_decompose(dirs)) <= 8

    def test_default_priority_is_3(self, sm):
        sectors = sm._heuristic_decompose([("xyzzy", 100)])
        assert sectors[0].priority == 3
        assert sectors[0].attack_surface == "Internal logic and data processing"


class TestParseSectorOutput:
    def test_parse_blocks(self, sm):
        sm._dir_stats = {"auth": 100, "api": 200}
        raw = """
SECTOR: Auth Module
PATH: auth
DESCRIPTION: login and session handling
ATTACK_SURFACE: authentication bypass
PRIORITY: 0

SECTOR: API Layer
PATH: api
DESCRIPTION: REST endpoints
ATTACK_SURFACE: injection
"""
        sectors = sm._parse_sector_output(raw)
        assert len(sectors) == 2
        assert sectors[0].priority == 0 and sectors[0].estimated_files == 100
        assert sectors[1].priority == 1 and sectors[1].estimated_files == 200  # 缺省按块序号
```

- [ ] **Step 2: 写 osv_bridge 测试**

`tests/test_osv_bridge.py`：

```python
"""engine/osv_bridge.py OSV 解析与转换（SPEC 5.3）。解析函数全为纯同步。"""

from pathlib import Path

import pytest

import engine.osv_bridge as osv


def _osv_doc(severity_db=None, cvss=None, aliases=None, pkg="libpng", version="1.6.0", fixed="1.6.1"):
    vuln = {
        "id": "OSV-2024-0001",
        "aliases": aliases or ["CVE-2024-0001"],
        "summary": "heap overflow",
        "affected": [
            {
                "package": {"name": pkg},
                "ranges": [{"events": [{"introduced": "0"}, {"fixed": fixed}]}],
            }
        ],
    }
    if severity_db:
        vuln["database_specific"] = {"severity": severity_db}
    if cvss:
        vuln["severity"] = [{"type": "CVSS_V3", "score": cvss}]
    return {
        "results": [
            {
                "source": {"type": "lockfile", "path": "requirements.txt"},
                "packages": [
                    {"package": {"name": pkg, "version": version}, "vulnerabilities": [vuln]}
                ],
            }
        ]
    }


class TestParseOsvJson:
    def test_fields_and_cvss_override(self):
        result = osv._parse_osv_json(_osv_doc(severity_db="MODERATE", cvss="CVSS:3.1/AV:N/9.8"), 1.5)
        assert len(result.vulnerabilities) == 1
        v = result.vulnerabilities[0]
        assert v.osv_id == "OSV-2024-0001"
        assert v.severity == "critical"  # CVSS 9.8 覆盖 database_specific 的 moderate
        assert v.cvss_score == pytest.approx(9.8)
        assert v.fixed_version == "1.6.1"
        assert result.scanned_files == ["lockfile:requirements.txt"]

    def test_normalize_and_cvss_mapping(self):
        assert osv._normalize_severity("severe") == "high"
        assert osv._normalize_severity("moderate") == "medium"
        assert osv._normalize_severity("bogus") == "medium"
        assert osv._cvss_to_severity(9.8) == "critical"
        assert osv._cvss_to_severity(7.0) == "high"
        assert osv._cvss_to_severity(4.0) == "medium"
        assert osv._cvss_to_severity(2.0) == "low"


class TestScanDependencies:
    async def test_binary_missing_returns_errors_not_raise(self, monkeypatch, tmp_path):
        monkeypatch.setattr(osv, "_find_osv_scanner", lambda: None)
        result = await osv.scan_dependencies(tmp_path)
        assert result.vulnerabilities == []
        assert any("not found" in e for e in result.errors)


class TestFindingsConversion:
    def test_dedup_and_cve_title(self):
        result = osv._parse_osv_json(_osv_doc(cvss="9.8"), 0.1)
        items = osv.osv_findings_to_blackboard(result)
        assert len(items) == 1
        item = items[0]
        assert item["title"] == "CVE-2024-0001"  # 优先 CVE alias
        assert item["finding_type"] == "dependency_vuln"
        assert item["hypothesis_id"] == "osv-OSV-2024-0001"
        assert item["package_name"] == "libpng"

    def test_context_empty_and_medium_only(self):
        empty = osv.OSVScanResult()
        assert osv.build_cerebrum_context(empty) == ""
        low_only = osv._parse_osv_json(_osv_doc(severity_db="MINOR"), 0.1)
        ctx = osv.build_cerebrum_context(low_only)
        assert "all medium/low severity" in ctx

    def test_context_critical_grouped(self):
        result = osv._parse_osv_json(_osv_doc(cvss="9.8"), 0.1)
        ctx = osv.build_cerebrum_context(result)
        assert "libpng@1.6.0" in ctx and "CVE-2024-0001" in ctx and "IMPORTANT" in ctx
```

- [ ] **Step 3: 写 exploit_analyzer 测试**

`tests/test_exploit_analyzer.py`：

```python
"""tools/exploit_analyzer.py 静态规则层与回退（SPEC 5.3）。

当前环境 LLM 层天然禁用（engine.llm_client / kimi_hive.llm_call 均不存在），
静态层行为即全部行为；monkeypatch _call_llm 可测 LLM 覆盖分支。
"""

from pathlib import Path

import pytest

from engine.blackboard import Finding
from tools.exploit_analyzer import _static_analysis, analyze_finding


def _finding(desc: str) -> Finding:
    return Finding(id="F-T1", hypothesis_id="H-T1", title=desc, description=desc, severity="high", evidence="")


class TestStaticAnalysis:
    def test_no_auth_and_system_privilege(self):
        r = _static_analysis("unauthenticated RCE via eval( in template rendering")
        assert r["auth_required"] is False  # no_auth 模式优先
        assert r["privilege_level"] == "system"  # eval( 命中 system 组

    def test_auth_required(self):
        r = _static_analysis("requires valid authentication token and admin session")
        assert r["auth_required"] is True

    def test_cve_only_from_desc(self):
        r = _static_analysis("see CVE-2024-12345 details", code="patch mentions CVE-1999-99999")
        assert r["cve_dependencies"] == ["CVE-2024-12345"]  # code 里的不提取

    def test_network_defaults_external(self):
        assert _static_analysis("some bug")["network_access"] == "external"
        assert _static_analysis("only reachable from localhost interface")["network_access"] == "localhost"


class TestAnalyzeFinding:
    def test_env_configs_without_target_env_conservative(self):
        f = _finding("overflow requires specific mmap_threshold configuration")
        r = analyze_finding(f, use_llm=False)
        if r.env_configs:  # 静态层识别出配置依赖时
            assert r.is_satisfiable is False  # 无 target_env → 保守判不可满足

    def test_target_env_satisfied(self):
        f = _finding("requires valid authentication token then overflow")
        r = analyze_finding(f, use_llm=False, target_env={"has_auth": True, "configs": []})
        assert r.is_satisfiable is True
        assert r.unmet_conditions == []

    def test_auth_unmet(self):
        f = _finding("requires valid authentication token then overflow")
        r = analyze_finding(f, use_llm=False, target_env={"has_auth": False, "configs": []})
        assert r.is_satisfiable is False
        assert any("auth" in c.lower() for c in r.unmet_conditions)
```

- [ ] **Step 4: 跑测试并修正（仅修测试）**

```bash
python -m pytest tests/test_sector_heuristics.py tests/test_osv_bridge.py tests/test_exploit_analyzer.py -v
```

预期：全 PASS。注意：`_static_analysis` 的正则命中细节（如 `mmap_threshold` 是否命中 env_configs、localhost 判定）以实际实现为准修正测试输入；约束是不改 `tools/exploit_analyzer.py` 本身。

- [ ] **Step 5: Commit**

```bash
git add tests/test_sector_heuristics.py tests/test_osv_bridge.py tests/test_exploit_analyzer.py
git commit -m "test: add sector/osv/exploit-analyzer unit tests (SPEC 5.3)"
```

---

### Task 12: 1c — fake-LLM 集成测试（端到端主循环）

**Files:**
- Create: `tests/fake_llm.py`（harness）
- Test: `tests/test_integration_cerebrum.py`
- Create: `tests/fixture_proj/src/app.py`（fixture 目标项目）

**Interfaces:**
- Produces: `FakeSession`（`prompt(user_input, *, merge_wire_messages=False)` 同步返回 async generator；`async close()`）；`make_fake_create(script) -> async callable`（替换 `kimi_agent_sdk.Session.create`）；`route_by_prompt(state, bb)` 返回 script 函数。后续 Phase 2 的 license 客户端测试也复用此 harness

已核实的关键机制（harness 设计依据）：
- `Session.create` 是 staticmethod；`mock.patch.object(kimi_agent_sdk.Session, "create", new=async_factory)` 一处拦截全部 4 个调用点（cerebrum 主 session、critic、drone、session_pool）
- chunk 必须是 SDK 真类：`from kimi_agent_sdk import TextPart`；cerebrum/drone 用 `isinstance(chunk, TextPart)` 判定
- prompt 路由：critic 含 `"CRITIC AGENT"`；drone 含 `"[DRONE TASK:"`；其余为 cerebrum 主循环
- drone 输出格式：`Drone._parse_round_output` 用正则提 `finding/severity/confidence/evidence/...`（大小写不敏感）
- `_wait_for_drain` 每轮真实 sleep(5.0) → patch 成 0.1s；`_inject_osv_findings` patch 掉（避免真跑 osv-scanner）
- `Drone._session_pool` 是类变量，测试间复位 `None`
- critic 任何异常/空输出默认 ACCEPT → fake critic 返回空串即可

- [ ] **Step 1: 写 harness**

`tests/fake_llm.py`：

```python
"""fake-LLM 集成测试 harness（SPEC 5.3）。

用法：
    monkeypatch.setattr(kimi_agent_sdk.Session, "create", make_fake_create(script))
script(prompt: str) -> str：按 prompt 内容路由到 cerebrum/drone/critic 预设输出。
"""

from __future__ import annotations

import json
from typing import Any, Callable

from kimi_agent_sdk import TextPart

DRONE_REPORT = """\
finding: Buffer overflow in parse_header via unchecked length prefix
severity: high
confidence: 0.9
evidence: parse_header copies length-prefixed token into 256-byte stack buffer without bounds check (app.py:12)
trace_needed: no
"""

ROUND1 = json.dumps(
    {
        "thinking": "recon done, one strong lead",
        "hypotheses": [
            {
                "claim": "parse_header copies token into fixed stack buffer without bounds check",
                "confidence": 0.8,
            }
        ],
        "tasks": [
            {
                "hypothesis_ref": "parse_header copies token",
                "role": "investigator",
                "description": "verify bounds check in parse_header",
            }
        ],
        "is_complete": False,
        "phase_complete": False,
    }
)

COMPLETE = json.dumps(
    {"thinking": "audit complete", "hypotheses": [], "tasks": [], "is_complete": True, "complete_reason": "done"}
)

EMPTY_ROUND = json.dumps({"thinking": "waiting for drone results", "hypotheses": [], "tasks": [], "is_complete": False})


class FakeSession:
    """最小接口：prompt() 同步返回 async generator + async close()。"""

    def __init__(self, script: Callable[[str], str]):
        self._script = script

    def prompt(self, user_input: str, *, merge_wire_messages: bool = False):
        return self._gen(user_input)

    async def _gen(self, user_input: str):
        yield TextPart(text=self._script(user_input))

    async def close(self) -> None:
        return None


def make_fake_create(script: Callable[[str], str]):
    async def fake_create(work_dir: Any = None, **kwargs: Any) -> FakeSession:
        return FakeSession(script)

    return fake_create


def default_script(state: dict, blackboard) -> Callable[[str], str]:
    """默认路由：R1 出假设+任务；finding 落黑板后下一轮声明完成；20 轮兜底防死循环。"""

    def script(prompt: str) -> str:
        if "CRITIC AGENT" in prompt:
            return ""  # critic 空输出 → 默认 ACCEPT
        if "[DRONE TASK:" in prompt:
            return DRONE_REPORT
        state["rounds"] = state.get("rounds", 0) + 1
        if state["rounds"] == 1:
            return ROUND1
        if blackboard.findings or state["rounds"] > 20:
            return COMPLETE
        return EMPTY_ROUND

    return script
```

- [ ] **Step 2: 写 fixture 目标项目**

`tests/fixture_proj/src/app.py`：

```python
"""fixture：故意埋缺陷的小目标，供集成测试扫描。"""

import ctypes


def parse_header(data: bytes) -> bytes:
    """从 data 读取长度前缀并拷贝到固定缓冲区（故意缺边界检查）。"""
    length = int.from_bytes(data[:4], "little")
    buf = ctypes.create_string_buffer(256)
    ctypes.memmove(buf, data[4 : 4 + length], length)  # length 未校验 → 溢出
    return buf.raw
```

- [ ] **Step 3: 写集成测试**

`tests/test_integration_cerebrum.py`：

```python
"""fake-LLM 端到端：假设→任务→drone→critic→finding→报告→终止（SPEC 5.3）。"""

import asyncio
from pathlib import Path

import pytest

import kimi_agent_sdk
from engine.blackboard import Blackboard
from engine.cerebrum import Cerebrum
from engine.drone import Drone
from fake_llm import DRONE_REPORT, default_script, make_fake_create  # tests/ 无 __init__.py，pytest prepend 模式直接导入

FIXTURE_PROJ = Path(__file__).parent / "fixture_proj"


async def _fast_drain(self, *args, **kwargs):
    await asyncio.sleep(0.1)


async def _noop_osv(self, *args, **kwargs):
    return None


@pytest.fixture(autouse=True)
def _reset_drone_pool():
    Drone._session_pool = None
    yield
    Drone._session_pool = None


class TestCerebrumEndToEnd:
    async def test_full_loop_produces_finding(self, tmp_path, monkeypatch):
        work = tmp_path / "work"
        root = tmp_path / "root"
        work.mkdir()
        root.mkdir()
        bb = Blackboard(work_dir=work)
        state: dict = {}

        monkeypatch.setattr(
            kimi_agent_sdk.Session, "create", make_fake_create(default_script(state, bb))
        )
        monkeypatch.setattr(Cerebrum, "_wait_for_drain", _fast_drain)
        monkeypatch.setattr(Cerebrum, "_inject_osv_findings", _noop_osv)  # async 方法必须用 async 替身，否则 await None 报错

        cerebrum = Cerebrum(
            blackboard=bb,
            work_dir=work,
            root_dir=root,
            config=None,
            max_rounds=25,
            max_tasks=50,
        )
        await asyncio.wait_for(cerebrum.launch(str(FIXTURE_PROJ)), timeout=120)

        assert len(bb.hypotheses) >= 1, "假设未生成"
        assert len(bb.tasks) >= 1, "任务未入队"
        assert len(bb.findings) >= 1, "drone→critic→finding 链路未闭合"
        finding = bb.findings[0]
        assert finding.severity == "high"
        assert "parse_header" in (finding.title + finding.description + finding.evidence)


class TestDroneUnit:
    async def test_drone_execute_and_parse(self, tmp_path, monkeypatch):
        monkeypatch.setattr(
            kimi_agent_sdk.Session, "create", make_fake_create(lambda p: DRONE_REPORT)
        )
        work = tmp_path / "w"
        root = tmp_path / "r"
        work.mkdir()
        root.mkdir()
        drone = Drone(
            task_id="T-TEST01",
            task_desc="verify bounds check in parse_header",
            drone_role="investigator",
            work_dir=work,
            root_dir=root,
            config=None,
        )
        result = await drone.execute()
        parsed = Drone._parse_round_output(result)
        assert parsed["severity"] == "high"
        assert float(parsed["confidence"]) == pytest.approx(0.9)
```

- [ ] **Step 4: 跑集成测试**

```bash
cd /home/fuzz/huntzero
python -m pytest tests/test_integration_cerebrum.py -v -s
```

预期：全 PASS（端到端约 3-10s）。排障指引：
- 若 `config=None` 在某处被解引用报错 → 改用 `kimi_hive.build_config("sk-fake")` 构造真实 Config（fake create 拦截后不会真连 LLM）
- 若 drone 结果迟迟未整合导致超轮：检查 dispatcher/integrator 的 1s 轮询节奏，把 `_fast_drain` 改为 `sleep(0.3)` 或把兜底轮数从 20 调到 40

- [ ] **Step 5: 全量回归 + Commit**

```bash
python -m pytest tests/ -q
git add tests/fake_llm.py tests/test_integration_cerebrum.py tests/fixture_proj/
git commit -m "test(integration): add fake-LLM end-to-end cerebrum loop harness (SPEC 5.3)"
```

---

### Task 13: 1c — 可疑点登记册 + 表征测试

**Files:**
- Create: `docs/suspicious-registry.md`
- Test: `tests/test_characterization.py`

**Interfaces:**
- Produces: 登记册文档（后续业务分析立项的输入）；xfail 表征测试（修复发生时 XPASS 报警）

原则（SPEC §8）：**只登记、只钉现状，不修**。`xfail(strict=True)` 用于钉"疑似错误"的现状——一旦被修复，测试 XPASS 失败，自动提醒移出登记册。

- [ ] **Step 1: 写登记册**

`docs/suspicious-registry.md`：

```markdown
# 可疑点登记册（Suspicious Points Registry）

原则：任何行为改动需要精准上下文和深入业务分析。以下均为静态分析/测试过程中
发现的疑似问题，**未经业务确认一律不修**。每项配表征测试钉住现状（可行时）。
处理流程：业务分析立项 → 确认是 bug → 修复并把对应 xfail 测试翻正。

| # | 位置 | 现象 | 发现途径 | 表征测试 | 状态 |
|---|------|------|---------|---------|------|
| 1 | `engine/drone.py:370` | `self._cleanup()` 缩进在 `async with` 块内，疑似每轮循环触发 | 代码审查 | 无（需业务理解） | 待分析 |
| 2 | `server/http.py` `_validate_project_path` | 返回注解引用未导入的 `Path`，调用时可能 NameError | 代码审查 | `test_http_validate_project_path_behavior` | 待分析 |
| 3 | `kimi_hive.py` FinOps 成本统计 | 模型名常量（kimi-long-context/kimi-latest）与实际 kimi-for-coding 不符 | 代码审查 | 无（需完整扫描触发） | 待分析 |
| 4 | `engine/cerebrum.py:3116` 扇区预算 | `getattr(sector, 'file_count', 0)` 恒为 0（Sector 只有 `estimated_files`），预算恒按 50 文件计算：P0=60/15、P1=48/12、P2=40/10、P3=32/10 | 测试签名提取 | 无（内联代码不可达） | 待分析 |
| 5 | `engine/cerebrum.py` `_total_tasks` | 解析器（:2027）与 dispatcher（:2492）双重递增，max_tasks 预算实际消耗 ×2 | 测试签名提取 | `test_task_queued_and_counted` 注释钉住 | 待分析 |
| 6 | `tools/poc_sandbox.py` DockerSandbox | `create_subprocess_exec` 未接 stdout/stderr PIPE，`stdout.decode()` 触发 AttributeError，docker 模式恒 ERROR | 测试签名提取 | `test_docker_sandbox_no_pipe_xfail` | 待分析 |
| 7 | `kimi.py` `feed` 子命令 | 空转占位（TODO: GitHub auto-feed） | 代码审查 | 无 | 待决定（保留/删除） |
```

- [ ] **Step 2: 探测 server/http.py 的实际行为**

```bash
cd /home/fuzz/huntzero
python - <<'EOF'
import inspect
import server.http as h
src = inspect.getsource(h._validate_project_path)
print(src[:600])
try:
    h._validate_project_path("/tmp/nonexistent-probe")
except Exception as e:
    print("RAISED:", type(e).__name__, e)
EOF
```

记录实际行为（NameError？还是正常返回？还是其他异常）。**下一步按探测结果二选一**，以观察到的为准。

- [ ] **Step 3: 写表征测试**

`tests/test_characterization.py`：

```python
"""可疑点表征测试（SPEC §8）：钉住现状行为，修复时 XPASS 报警。

xfail(strict=True) 语义：当前表现被钉为"失败预期"；一旦被修复，
XPASS 会导致套件失败 → 提醒把条目移出登记册并翻正测试。
"""

from pathlib import Path

import pytest


class TestDockerSandboxNoPipe:
    """登记册 #6：DockerSandbox 未接 stdout/stderr PIPE，docker 模式恒 ERROR。"""

    @pytest.mark.xfail(strict=True, reason="registry#6: docker sandbox stdout 未接 PIPE")
    async def test_docker_sandbox_should_capture_output(self, tmp_path, monkeypatch):
        from tools.poc_sandbox import DockerSandbox

        poc = tmp_path / "poc.py"
        poc.write_text("print('ok')", encoding="utf-8")

        class _FakeProc:
            returncode = 0
            pid = 12345

            async def communicate(self):
                return (None, None)  # 现状：未接 PIPE → None

            async def wait(self):
                return 0

        async def fake_exec(*args, **kwargs):
            return _FakeProc()

        async def _noop_image(self):
            return None

        monkeypatch.setattr("asyncio.create_subprocess_exec", fake_exec)
        # _ensure_image 若为 async 方法必须用 async 替身；若为同步则改用 lambda self: None
        monkeypatch.setattr(DockerSandbox, "_ensure_image", _noop_image)
        result = await DockerSandbox().execute(poc, target="http://example.local")
        # 正确行为应是 VERIFIED_SUCCESS 且捕获输出；现状是 ERROR
        assert result.status.value == "verified_success"


class TestHttpValidateProjectPath:
    """登记册 #2：按 Step 2 探测结果钉住 _validate_project_path 现状。

    若探测为 NameError：用 xfail 钉住（示例如下，strict）；
    若探测为其他行为：改写为对实际行为的直接断言。
    """

    @pytest.mark.xfail(strict=True, reason="registry#2: Path 未导入")
    def test_validate_project_path_should_work(self):
        from server.http import _validate_project_path

        # 调用签名以 server/http.py 实际源码为准（Step 2 已打印）
        result = _validate_project_path("/tmp/nonexistent-probe")
        assert result is not None
```

- [ ] **Step 4: 跑表征测试确认 xfail 成立**

```bash
python -m pytest tests/test_characterization.py -v
```

预期：两个测试均为 **XFAIL**（不是 PASS 也不是 FAIL）。若出现 XPASS → 现状已变化，更新登记册状态。

- [ ] **Step 5: Commit**

```bash
git add docs/suspicious-registry.md tests/test_characterization.py
git commit -m "test: pin suspicious-point behaviors with xfail characterization tests (SPEC 8)"
```

---

### Task 14: 1a/1c — semantic_helper 合并（engine ← tools，先表征后合并）

**Files:**
- Test: `tests/test_semantic_helper.py`
- Modify: `engine/semantic_helper.py`（并入 tools 版独有功能）
- Delete: `tools/semantic_helper.py`
- Modify: 引用点（README、`.bots.md` 若指向 tools 版）

**Interfaces:**
- Produces: `engine/semantic_helper.py` 为唯一语义分析 CLI，子命令集合 = 两版并集；`.bots.md:34` 已指向 engine 版（无需改）

背景：`engine/semantic_helper.py`（574 行，`.bots.md:34` 指示 drone 调用）与 `tools/semantic_helper.py`（~750 行，多污点追踪）功能重复。合并方向：**engine 版为基座，并入 tools 版独有子命令**。

- [ ] **Step 1: 采集两版 golden 输出（合并前）**

```bash
cd /home/fuzz/huntzero
echo "== engine 版子命令 =="; python engine/semantic_helper.py 2>&1 | head -20
echo "== tools 版子命令 =="; python tools/semantic_helper.py --help 2>&1 | head -20
python engine/semantic_helper.py list_functions tests/fixture_proj/src/app.py > /tmp/golden_engine_listfunc.txt || true
python tools/semantic_helper.py list_functions tests/fixture_proj/src/app.py > /tmp/golden_tools_listfunc.txt || true
diff /tmp/golden_engine_listfunc.txt /tmp/golden_tools_listfunc.txt && echo "输出一致" || echo "输出有差异（合并时以 engine 版为准）"
```

记录两版各自独有的子命令清单（写入 commit message）。

- [ ] **Step 2: 写 CLI 表征/功能测试**

`tests/test_semantic_helper.py`：

```python
"""engine/semantic_helper.py CLI 表征测试（SPEC 5.1 合并守卫）。"""

import subprocess
import sys
from pathlib import Path

ENGINE_HELPER = Path(__file__).parent.parent / "engine" / "semantic_helper.py"
FIXTURE = Path(__file__).parent / "fixture_proj" / "src" / "app.py"


def _run(*args: str) -> subprocess.CompletedProcess:
    return subprocess.run(
        [sys.executable, str(ENGINE_HELPER), *args],
        capture_output=True,
        text=True,
        timeout=60,
    )


class TestSemanticHelperCLI:
    def test_list_functions_finds_fixture_func(self):
        r = _run("list_functions", str(FIXTURE))
        assert r.returncode == 0
        assert "parse_header" in r.stdout

    def test_taint_subcommand_available(self):
        """tools 版并入后，污点追踪子命令必须存在于 engine 版。"""
        r = _run("--help")
        help_text = r.stdout + r.stderr
        # 以 Step 1 采集的 tools 版独有子命令名为准（如 taint/data_flow/trace）
        assert any(kw in help_text for kw in ("taint", "data_flow", "trace"))
```

- [ ] **Step 3: 执行合并**

1. 通读两版源码，列出 tools 版独有的函数/子命令（预期：污点追踪 `taint`/`data_flow` 相关，约 150-250 行）
2. 逐函数移植到 `engine/semantic_helper.py`（保留 engine 版的 CLI 入口结构与子命令注册方式）
3. 处理 import 差异（两版都应是自包含 CLI，无 engine 内部依赖）
4. `git rm tools/semantic_helper.py`
5. 全仓 grep 残留引用：`grep -rn "tools/semantic_helper" --include="*.py" --include="*.md" . | grep -v docs/superpowers` → 逐一改为 `engine/semantic_helper.py`（README、`skills/**/*.md` 若提及）

**边界**：只合并 CLI 功能，不改变两个版本对同一子命令的输出格式（`.bots.md` 的 drone 提示词依赖它）。

- [ ] **Step 4: 验证**

```bash
python -m pytest tests/test_semantic_helper.py -v
python engine/semantic_helper.py list_functions tests/fixture_proj/src/app.py | diff - /tmp/golden_engine_listfunc.txt && echo "golden 一致"
python -m pytest tests/ -q
```

预期：测试 PASS；golden 输出逐字节一致；全量绿。

- [ ] **Step 5: Commit**

```bash
git add engine/semantic_helper.py tests/test_semantic_helper.py
git rm tools/semantic_helper.py 2>/dev/null || true
git add -A
git commit -m "refactor(tools): merge tools/semantic_helper into engine version (SPEC 5.1)"
```

---

### Task 15: 1d — 品牌统一 huntzero + kimi.py → huntzero.py

**Files:**
- Rename: `kimi.py` → `huntzero.py`
- Modify: `pyproject.toml`（scripts entry）、`.bots.md`（标题与自称）、`server/mcp.py`（server 标识）、`web_ui_v2/`（标题/包名）、`kimi_hive.py`（报告落款与横幅字符串）、`README.md`（品牌字段，全文重写在 Task 17）

**Interfaces:**
- Produces: `huntzero` CLI（`pip install -e .` 后可用）；品牌字符串统一

边界（SPEC §5.4）：
- `KIMI_*` 环境变量名**不改**（SDK 契约）
- Redis key 命名空间 `kimisec:*` **不改**（内部协议，改动无收益有兼容风险）
- Python 包名 `engine/`、`server/` 不动；模块 `kimi_hive.py` 保留（改名收益低风险高，其内部字符串改品牌即可）
- 只改面向用户的品牌字符串，不改逻辑

- [ ] **Step 1: 改名与 import 修正**

```bash
cd /home/fuzz/huntzero
git mv kimi.py huntzero.py
grep -rn "import kimi$\|from kimi import\|import kimi " --include="*.py" . || echo "(无对 kimi 模块的 import)"
grep -rn "kimi\.py\|kimi:main\|python kimi" --include="*.py" --include="*.yml" --include="*.toml" --include="Dockerfile*" . | grep -v docs/superpowers || echo "(无路径引用)"
```

对命中的每一处改为 `huntzero`（预期命中：Dockerfile CMD、docker-compose command、`pyproject.toml` scripts）。`pyproject.toml` 的 `[project.scripts]` 改为 `huntzero = "huntzero:main"`，`[tool.setuptools] py-modules` 中 `"kimi"` 改为 `"huntzero"`。

- [ ] **Step 2: 品牌字符串替换**

```bash
grep -rln "kimiSec\|KimiSec\|HIVE-MIND" --include="*.py" --include="*.md" --include="*.ts" --include="*.vue" --include="*.html" --include="*.json" . \
  | grep -v docs/superpowers | grep -v .git
```

逐文件处理（先看上下文再改）：
- `.bots.md`：标题 `# Agent 指令 — kimiSec V8 ...` → `# Agent 指令 — huntzero V8 ...`；正文中自称替换
- `server/mcp.py`：JSON-RPC server info 的 name/version 字段
- `kimi_hive.py`：启动横幅、报告头部落款
- `web_ui_v2/`：`index.html` title、`package.json` name、侧栏/页头组件中的产品名
- `server/http.py`：页面/提示语中的产品名

注意：`kimi-for-coding`（模型名）、`kimi_agent_sdk`/`kimi_cli`/`kosong`（SDK 包名）、`api.kimi.com`（端点）**不是品牌字符串，绝不动**。替换前后各跑一次 `grep` 对照清单，防止误伤。

- [ ] **Step 3: 验证**

```bash
pip install -e ".[dev]"
huntzero --help | head -5
python huntzero.py --help | head -5
python -m pytest tests/ -q
```

预期：CLI 可用；pytest 全绿（env sync 测试不受品牌替换影响——若失败说明误改了 `KIMI_*`，立即回查）。

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "refactor(brand): rename kimi.py to huntzero.py and unify branding (SPEC 5.4)"
```

---

### Task 16: 1d — Nuitka 打包 spike

**Files:**
- Create: `docs/nuitka-spike.md`（结论报告）
- Create: `build/`（产物目录，gitignore 已覆盖）

**Interfaces:**
- Consumes: Task 15 后的 `huntzero.py`
- Produces: spike 结论（主路线可行 / 落 Cython fallback），写入 `docs/nuitka-spike.md`

- [ ] **Step 1: 安装与编译**

```bash
cd /home/fuzz/huntzero
pip install nuitka ordered-set zstandard
python -m nuitka --standalone \
  --include-package=engine,server,tools \
  --include-package-data=skills \
  --output-dir=build/nuitka \
  --assume-yes-for-downloads \
  huntzero.py 2>&1 | tail -30
```

预期：`build/nuitka/huntzero.dist/huntzero` 生成。常见排障：缺包 → 按报错追加 `--include-package=<name>`；tree-sitter 原生库 → 确认各 `tree_sitter_*` 包被 follow；耗时 5-20 分钟正常。

注意：`skills/` 是运行时按路径读取的数据目录（`engine/cerebrum.py:1037`），`--include-package-data` 只对包内数据生效——若二进制运行时找不到 `skills/`， spike 报告中记录"skills 需随二进制外置分发"（与 vault 设计一致，本来就是外置的）。

- [ ] **Step 2: 冒烟验证**

```bash
./build/nuitka/huntzero.dist/huntzero --help
echo "---"
KIMI_API_KEY=sk-dummy ./build/nuitka/huntzero.dist/huntzero scan --help 2>&1 | head -10
echo "---源码回归---"
python -m pytest tests/ -q
```

预期：`--help` 正常输出（证明解释器/依赖/动态导入解析成功）；scan 子命令帮助可达；源码 pytest 不受影响。

- [ ] **Step 3: 写 spike 报告**

`docs/nuitka-spike.md`：

```markdown
# Nuitka 打包 spike 报告（2026-07-29）

## 结论
[ ] 主路线可行 —— standalone 二进制冒烟通过，Phase 2 走 Nuitka
[ ] 落 fallback —— 原因：____，Phase 2 改 Cython + python-build-standalone

## 构建参数
（记录最终成功的完整 nuitka 命令行）

## 冒烟结果
（--help / scan --help 输出摘要）

## 已知问题与外置资源
- skills/ 目录：外置分发（与 skills vault 设计一致）
- tree-sitter / osv-scanner / 其他二进制依赖：（记录）

## 体积与启动耗时
（du -sh huntzero.dist；time ./huntzero --help）
```

- [ ] **Step 4: Commit**

```bash
git add docs/nuitka-spike.md
git commit -m "docs(spike): nuitka packaging feasibility report (SPEC 5.4)"
```

---

### Task 17: 1d — README.md / AGENTS.md 重写（现状化）

**Files:**
- Modify: `README.md`（全文重写）
- Modify: `AGENTS.md`（全文重写）

说明：SPEC §6.5 的对外文档（部署/操作/运维/白皮书）属 Phase 2；本任务只做**仓库内文档现状化**，让 Phase 1 之后的协作者拿到准确信息。

- [ ] **Step 1: 重写 README.md**

结构（内容以当前仓库事实为准，品牌 huntzero）：

```markdown
# huntzero

LLM 主导的假设驱动漏洞挖掘引擎。私有化交付（VM 镜像 + Web 控制台）。

> 商业化设计总纲：docs/superpowers/specs/2026-07-29-huntzero-productionization-design.md
> License: Proprietary（专有软件，非开源）

## 架构
（Cerebrum 主脑 / Drone 工蜂 / Blackboard 黑板 / Sector 分区，4-6 行概述 + engine/ server/ skills/ 目录职责）

## 开发环境
pip install -e ".[dev]"
python -m pytest            # 全量测试
python tools/check_env_sync.py  # env 一致性校验

## 运行（开发模式）
export KIMI_API_KEY=sk-...
python huntzero.py scan https://github.com/owner/repo
python huntzero.py serve    # MCP + HTTP 控制台
python huntzero.py worker   # Redis 集群 worker

## 测试体系
（单测清单 + fake-LLM 集成测试一行说明 + 表征测试/登记册约定，指向 docs/suspicious-registry.md）
```

删除的旧内容：`index/` 官网、lieling.xyz、`INTEL_SECRET`/`WECOM_WEBHOOK`、`knownAttack/`、`model_q/`、`patchdiff/`、MIT 声明、`vuln-hunting-plugin` 引用。

- [ ] **Step 2: 重写 AGENTS.md**

```markdown
# Repository Guidelines（huntzero）

## 结构
huntzero.py（CLI 入口）/ kimi_hive.py（扫描编排）/ engine/（cerebrum·drone·blackboard·sector·backends·llm_config·osv_bridge·fuzzer·semantic_helper）/ server/（core·http·mcp·worker）/ skills/（领域情报库）/ tools/（poc_sandbox·exploit_analyzer·finops_monitor·check_env_sync·install_osv）/ tests/

## 命令
pip install -e ".[dev]" / python -m pytest / python tools/check_env_sync.py / black --line-length 100 . / ruff check .

## 约定
- Python 3.11+，black 100 列，ruff E,F,W,I,N,UP；pytest-asyncio auto 模式
- KIMI_* 环境变量名不动（SDK 契约）；新增配置必须同步 .env.example（test_env_sync 强制）
- 疑似 bug 不修，进 docs/suspicious-registry.md + xfail 表征测试
- 运行时产物（.blackboard.json、local_workspace/ 等）不入库

## 提交
conventional commits；每个改动跑全量 pytest
```

- [ ] **Step 3: 验证 + Commit**

```bash
grep -n "lieling\|INTEL_SECRET\|WECOM\|MIT\|knownAttack\|model_q" README.md AGENTS.md || echo "(旧内容已清)"
git add README.md AGENTS.md
git commit -m "docs: rewrite README and AGENTS for huntzero (SPEC 5.4/6.5)"
```

---

### Task 18: Phase 1 验收门禁

**Files:** 无（纯验证）

- [ ] **Step 1: 全量质量门禁**

```bash
cd /home/fuzz/huntzero
python -m pytest tests/ -q
python tools/check_env_sync.py
black --check --line-length 100 . || true
ruff check . || true
git status --short
```

预期：pytest 全绿；env sync OK；`git status` 干净。black/ruff 告警只记录不强行清零（避免大面积格式化引入行为风险；新写代码应无告警）。

- [ ] **Step 2: SPEC §3 Phase 1 成功标准核对**

逐条核对：
- [ ] pytest 全绿，覆盖 SPEC §5.3 清单（blackboard 去重/Bayesian/backends/cerebrum 解析/TerminationGuard/sector/osv/exploit_analyzer）
- [ ] fake-LLM 集成测试跑通主循环（假设→任务→drone→critic→finding→终止）
- [ ] `.env.example` 与代码读取点一一对应（CI 校验脚本在）
- [ ] 仓库无死文件（SPEC §5.1 三张清单全部处理）
- [ ] Nuitka spike 有结论（`docs/nuitka-spike.md`）
- [ ] 可疑点登记册建立（`docs/suspicious-registry.md`，7 项初始条目）

- [ ] **Step 3: 打 tag**

```bash
git tag phase1-complete
```

（push 由用户执行。）Phase 2（license 客户端 + 网关 + VM 流水线）按 SPEC §6 另行出实施计划。
