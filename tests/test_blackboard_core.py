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
        t1 = await bb.add_task(
            h, "check parse_header bounds validation logic", drone_role="investigator"
        )
        t2 = await bb.add_task(
            h, "check parse_header bounds validation logic", drone_role="exploit-crafter"
        )
        assert t1 is not None and t2 is not None  # 去重仅限同 role


# ───────────── 发现合并 ─────────────


class TestFindingMerge:
    async def test_same_hypothesis_finding_merges_with_severity_upgrade(self, bb):
        h = await bb.add_hypothesis("overflow in parse_header when length unchecked", 0.6)
        f1 = await bb.add_finding(h, "parse_header overflow", "desc a", "medium", "evidence a")
        f2 = await bb.add_finding(
            h, "parse_header overflow different", "desc b", "high", "evidence b"
        )
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
