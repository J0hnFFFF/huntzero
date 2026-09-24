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
