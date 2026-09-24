"""TerminationGuard 五层终止判定（SPEC 5.3）。L1 在解析器；L5 为传输层超时兜底，不在本类。"""

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
