"""engine/sector.py 启发式回退分区与 LLM 输出解析（SPEC 5.3）。

对齐 tests/test_sector.py 直测私有方法的惯例。
动态预算公式已提取为 engine/cerebrum.py 的 _compute_sector_budget（registry #4），
由 tests/test_sector_budget.py 覆盖，不在本文件测。
"""

from pathlib import Path

import pytest

from engine.blackboard import Blackboard
from engine.sector import SectorManager


@pytest.fixture
def sm() -> SectorManager:
    return SectorManager(
        project_root=Path("."), blackboard=Blackboard(work_dir=Path(".")), config=None
    )


class TestHeuristicDecompose:
    def test_skip_and_priority_and_limit(self, sm):
        dirs = [
            ("docs", 300),  # skip_names 精确匹配 → 跳过
            ("auth", 200),  # P0（auth 命中）
            ("network", 150),  # P0（子串 "net" 先命中）
            ("utils", 30),  # P3
            ("tiny", 10),  # < 25 → 跳过
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
        # 缺省 PRIORITY 取 enumerate 原始块序号（含被跳过的前导空块）→ 2，钉住现状
        assert sectors[1].priority == 2 and sectors[1].estimated_files == 200
