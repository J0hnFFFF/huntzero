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
        # tools 版独有子命令名（data_flow 等）必须出现在 usage 中
        assert any(kw in help_text for kw in ("taint", "data_flow", "trace"))
