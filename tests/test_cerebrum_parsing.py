"""engine/cerebrum.py 输出解析（SPEC 5.3）。直调 _parse_json_output/_parse_output。"""

from pathlib import Path

import pytest

from engine.blackboard import Blackboard
from engine.cerebrum import Cerebrum


@pytest.fixture
def cerebrum(tmp_path: Path) -> Cerebrum:
    bb = Blackboard(work_dir=tmp_path)
    return Cerebrum(blackboard=bb, work_dir=tmp_path, root_dir=tmp_path, config=None)


# ───────────── hypotheses 过滤 ─────────────


class TestHypothesisParsing:
    async def test_valid_hypothesis_added(self, cerebrum):
        done = await cerebrum._parse_json_output(
            {
                "hypotheses": [
                    {"claim": "overflow in parse_header when length unchecked", "confidence": 0.8}
                ]
            }
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
            {
                "hypotheses": [
                    {"claim": "This is not exploitable and poses no risk", "confidence": 0.9}
                ]
            }
        )
        assert len(cerebrum.blackboard.hypotheses) == 0

    async def test_confidence_clamped(self, cerebrum):
        await cerebrum._parse_json_output(
            {
                "hypotheses": [
                    {"claim": "overflow in parse_header when length unchecked", "confidence": 3.7}
                ]
            }
        )
        node = next(iter(cerebrum.blackboard.hypotheses.values()))
        assert node.confidence == pytest.approx(1.0)


# ───────────── tasks 入队 ─────────────


class TestTaskParsing:
    async def test_parser_does_not_count_tasks(self, cerebrum):
        # registry #5 回归：任务计数已单点化到 _drone_dispatcher，
        # 解析器侧不再递增 _total_tasks。
        await cerebrum._parse_json_output(
            {
                "hypotheses": [
                    {"claim": "overflow in parse_header when length unchecked", "confidence": 0.8}
                ],
                "tasks": [
                    {
                        "hypothesis_ref": "overflow in parse_header",
                        "role": "investigator",
                        "description": "verify bounds check in parse_header",
                    }
                ],
            }
        )
        assert cerebrum._total_tasks == 0
        assert cerebrum._task_queue.qsize() == 1

    async def test_empty_description_skipped(self, cerebrum):
        await cerebrum._parse_json_output(
            {
                "hypotheses": [
                    {"claim": "overflow in parse_header when length unchecked", "confidence": 0.8}
                ],
                "tasks": [
                    {"hypothesis_ref": "overflow", "role": "investigator", "description": ""}
                ],
            }
        )
        assert cerebrum._task_queue.qsize() == 0


# ───────────── findings 硬门槛 ─────────────


class TestFindingParsing:
    async def test_low_confidence_finding_rejected(self, cerebrum):
        await cerebrum._parse_json_output(
            {
                "findings": [
                    {
                        "title": "parse_header overflow",
                        "description": "d",
                        "severity": "high",
                        "evidence": "e",
                        "confidence": 0.5,
                    }
                ]
            }
        )
        assert len(cerebrum.blackboard.findings) == 0  # < 0.70 硬门槛

    async def test_finding_accepted_and_severity_normalized(self, cerebrum):
        await cerebrum._parse_json_output(
            {
                "findings": [
                    {
                        "title": "parse_header overflow",
                        "description": "d",
                        "severity": "bogus",
                        "evidence": "e",
                        "confidence": 0.9,
                    }
                ]
            }
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
                    {
                        "claim": "critical overflow in parse_header with code ref at parser.c:42",
                        "confidence": 0.95,
                    }
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
        done = await cerebrum._parse_json_output(
            {"is_complete": True, "complete_reason": "audit done"}
        )
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
