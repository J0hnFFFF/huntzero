"""
Tests for the Sector-Based Architecture (engine/sector.py + BlackboardPartition).

Validates:
  - Project scale detection
  - Heuristic decomposition
  - BlackboardPartition isolation (hypotheses/tasks independent per sector)
  - Finding bubble-up (findings auto-aggregate to parent Blackboard)
  - sector_id tagging
  - Backward compatibility (existing Blackboard API unchanged)
"""
import asyncio
from pathlib import Path

import pytest

from engine.blackboard import Blackboard, BlackboardPartition, Finding, HypothesisStatus
from engine.sector import Sector, SectorManager


# ─────────────────────────────────────────────
#  Fixtures
# ─────────────────────────────────────────────

@pytest.fixture
def blackboard(tmp_path: Path) -> Blackboard:
    bb = Blackboard(work_dir=tmp_path)
    bb.target = "test-project"
    return bb


@pytest.fixture
def partition_pair(blackboard: Blackboard):
    """Create two independent partitions for testing isolation."""
    p1 = blackboard.create_partition("sector-auth")
    p2 = blackboard.create_partition("sector-api")
    return p1, p2


# ─────────────────────────────────────────────
#  BlackboardPartition Tests
# ─────────────────────────────────────────────

class TestBlackboardPartition:

    async def test_hypothesis_isolation(self, partition_pair, blackboard):
        """Hypotheses added to partitions should NOT appear in parent or sibling."""
        p1, p2 = partition_pair

        h1 = await p1.add_hypothesis("SQL injection in login form", 0.7)
        h2 = await p2.add_hypothesis("SSRF in API proxy endpoint", 0.6)

        assert len(p1.hypotheses) == 1
        assert len(p2.hypotheses) == 1
        assert len(blackboard.hypotheses) == 0  # parent is unaffected

    async def test_task_isolation(self, partition_pair, blackboard):
        """Tasks added to partitions should NOT appear in parent or sibling."""
        p1, p2 = partition_pair

        h1 = await p1.add_hypothesis("test hypothesis", 0.5)
        t1 = await p1.add_task(h1, "check auth bypass", "code-understander")

        h2 = await p2.add_hypothesis("test hypothesis 2", 0.5)
        t2 = await p2.add_task(h2, "check SSRF chain", "vuln-hunter")

        assert len(p1.tasks) == 1
        assert len(p2.tasks) == 1
        assert len(blackboard.tasks) == 0  # parent is unaffected

    async def test_finding_bubble_up(self, partition_pair, blackboard):
        """Findings should be written to both partition AND parent Blackboard."""
        p1, p2 = partition_pair

        h1 = await p1.add_hypothesis("SQLi hypothesis", 0.7)
        h2 = await p2.add_hypothesis("SSRF hypothesis", 0.6)

        f1 = await p1.add_finding(h1, "SQLi in login", "desc1", "high", "evidence1")
        f2 = await p2.add_finding(h2, "SSRF in proxy", "desc2", "critical", "evidence2")

        # Parent has ALL findings
        assert len(blackboard.findings) == 2

        # Each partition caches its own
        assert len(p1.findings) == 1
        assert len(p2.findings) == 1

    async def test_finding_sector_id_tagging(self, partition_pair, blackboard):
        """Findings bubbled up to parent should carry the sector_id."""
        p1, p2 = partition_pair

        h1 = await p1.add_hypothesis("test hypo", 0.5)
        await p1.add_finding(h1, "test finding", "d", "high", "e")

        assert blackboard.findings[0].sector_id == "sector-auth"

    async def test_hypothesis_dedup_within_partition(self, partition_pair):
        """Duplicate hypotheses within the same partition should be merged."""
        p1, _ = partition_pair

        h1 = await p1.add_hypothesis("SQL injection in the login form", 0.5)
        h2 = await p1.add_hypothesis("SQL injection in login form endpoint", 0.7)

        # Should have been merged (high similarity)
        assert len(p1.hypotheses) == 1

    async def test_task_dedup_within_partition(self, partition_pair):
        """Duplicate tasks within the same partition should be rejected."""
        p1, _ = partition_pair

        h = await p1.add_hypothesis("test", 0.5)
        t1 = await p1.add_task(h, "grep for SQL injection patterns", "vuln-hunter")
        t2 = await p1.add_task(h, "grep for SQL injection patterns in auth", "vuln-hunter")

        assert t1 is not None
        assert t2 is None  # rejected as duplicate

    async def test_partition_stats(self, partition_pair):
        """stats() should reflect only the partition's own state."""
        p1, _ = partition_pair

        h = await p1.add_hypothesis("test", 0.5)
        await p1.add_task(h, "test task", "general")

        stats = p1.stats()
        assert stats["hypotheses"] == 1
        assert stats["sector_id"] == "sector-auth"

    async def test_partition_snapshot(self, partition_pair):
        """snapshot() should include sector_id."""
        p1, _ = partition_pair

        snap = p1.snapshot()
        assert snap["sector_id"] == "sector-auth"

    async def test_hypothesis_confirmed_on_finding(self, partition_pair):
        """Adding a finding should auto-confirm the linked hypothesis in the partition."""
        p1, _ = partition_pair

        h = await p1.add_hypothesis("vuln hypo", 0.7)
        await p1.add_finding(h, "vuln confirmed", "d", "high", "e")

        assert p1.hypotheses[h].status == HypothesisStatus.CONFIRMED


# ─────────────────────────────────────────────
#  Backward Compatibility Tests
# ─────────────────────────────────────────────

class TestBackwardCompatibility:

    async def test_add_finding_without_sector_id(self, blackboard):
        """Original add_finding() call (no sector_id) should still work."""
        h = await blackboard.add_hypothesis("test", 0.5)
        f_id = await blackboard.add_finding(h, "title", "d", "high", "e")
        assert blackboard.findings[0].sector_id is None

    async def test_add_finding_with_sector_id(self, blackboard):
        """add_finding() now accepts optional sector_id."""
        f_id = await blackboard.add_finding(
            "h1", "title", "d", "high", "e", sector_id="sector-x"
        )
        assert blackboard.findings[0].sector_id == "sector-x"

    async def test_old_data_load(self, blackboard):
        """Old persisted data without sector_id should load correctly."""
        old_finding_data = {
            "id": "F-OLD",
            "hypothesis_id": "h",
            "title": "old finding",
            "description": "d",
            "severity": "low",
            "evidence": "e",
            "created_at": 0.0,
            # No sector_id
        }
        f = Finding(**{
            k: v for k, v in old_finding_data.items()
            if k in Finding.__dataclass_fields__
        })
        assert f.sector_id is None


# ─────────────────────────────────────────────
#  SectorManager Tests
# ─────────────────────────────────────────────

class TestSectorManager:

    def test_heuristic_decompose_prioritizes_security_dirs(self):
        """Directories matching security patterns should get higher priority."""
        sorted_dirs = [
            ("utils", 200),
            ("auth", 100),
            ("api", 150),
            ("docs", 300),
            ("tests", 500),
            ("core", 80),
            ("config", 50),
        ]

        sm = SectorManager(
            project_root=Path("."),
            blackboard=Blackboard(work_dir=Path(".")),
            config=None,
        )

        sectors = sm._heuristic_decompose(sorted_dirs)
        names = [s.name for s in sectors]

        # auth and api should appear before utils/core
        assert "Auth" in names
        assert "Api" in names
        # docs and tests should be excluded
        assert "Docs" not in names
        assert "Tests" not in names

        # auth/api should have lower priority number (= higher priority)
        auth_sector = next(s for s in sectors if s.name == "Auth")
        utils_sector = next((s for s in sectors if s.name == "Utils"), None)
        assert auth_sector.priority < 3

    def test_heuristic_skips_small_dirs(self):
        """Directories with too few files should be excluded."""
        sorted_dirs = [
            ("auth", 10),    # too small (< SECTOR_IDEAL_MIN_FILES // 2 = 25)
            ("api", 100),
        ]

        sm = SectorManager(
            project_root=Path("."),
            blackboard=Blackboard(work_dir=Path(".")),
            config=None,
        )

        sectors = sm._heuristic_decompose(sorted_dirs)
        names = [s.name for s in sectors]
        assert "Auth" not in names
        assert "Api" in names

    def test_max_sectors_limit(self):
        """Should never produce more than MAX_SECTORS."""
        sorted_dirs = [(f"dir{i}", 100) for i in range(20)]

        sm = SectorManager(
            project_root=Path("."),
            blackboard=Blackboard(work_dir=Path(".")),
            config=None,
        )

        sectors = sm._heuristic_decompose(sorted_dirs)
        assert len(sectors) <= 8  # MAX_SECTORS

    def test_sector_output_parsing(self):
        """_parse_sector_output should parse well-formatted LLM output."""
        sm = SectorManager(
            project_root=Path("."),
            blackboard=Blackboard(work_dir=Path(".")),
            config=None,
        )
        sm._dir_stats = {"auth": 100, "api": 200}

        raw = """
SECTOR: Authentication Module
PATH: auth
DESCRIPTION: Handles user login and session management
ATTACK_SURFACE: HTTP POST with credentials
PRIORITY: 0

SECTOR: API Gateway
PATH: api
DESCRIPTION: REST API endpoints for external consumers
ATTACK_SURFACE: All external HTTP requests
PRIORITY: 1
"""
        sectors = sm._parse_sector_output(raw)
        assert len(sectors) == 2
        assert sectors[0].name == "Authentication Module"
        assert sectors[0].path == "auth"
        assert sectors[0].priority == 0
        assert sectors[1].name == "API Gateway"
        assert sectors[1].path == "api"
