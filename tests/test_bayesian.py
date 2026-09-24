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
        # 修正版：_update_strength_for_pair 要求 parent 与 child 都有 outcome
        # 记录才更新矩阵，且 n ≥ 3 才计入 learned_pairs——先记 parent outcome，
        # 再对同一 child 记 3 次（n=3 → 达到 MIN_SAMPLES_TO_USE_LEARNED）。
        engine = BayesianConfidenceEngine(bb.hypotheses, strength_path=tmp_path)
        parent = await bb.add_hypothesis("parser alpha has unchecked length prefix", 0.8)
        child = await bb.add_hypothesis(
            "length prefix leads to heap overflow in token copy", 0.5, parent_id=parent
        )
        engine.record_outcome(parent, confirmed=True)
        for _ in range(3):
            engine.record_outcome(child, confirmed=True)
        stats = engine.get_strength_stats()
        assert stats["learned_pairs"] >= 1
        assert engine.save() is True
        assert (tmp_path / ".bayesian_strength.json").exists()

    def test_save_without_path_returns_false(self, bb):
        assert BayesianConfidenceEngine(bb.hypotheses).save() is False
