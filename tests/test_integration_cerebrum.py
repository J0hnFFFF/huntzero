"""fake-LLM 端到端：假设→任务→drone→critic→finding→报告→终止（SPEC 5.3）。"""

import asyncio
from pathlib import Path

import kimi_agent_sdk
import pytest
from fake_llm import (  # tests/ 无 __init__.py，pytest prepend 模式直接导入
    DRONE_REPORT,
    default_script,
    make_fake_create,
)

from engine.blackboard import Blackboard
from engine.cerebrum import Cerebrum
from engine.drone import Drone

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
        monkeypatch.setattr(
            Cerebrum, "_inject_osv_findings", _noop_osv
        )  # async 方法必须用 async 替身，否则 await None 报错

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
        # registry #5 回归（fix-batch2 实测分解）：_total_tasks 只计 dispatcher
        # 实际派发数 = ROUND1 investigator ×1 + auto-PoC ×1 + Devil's Advocate ×1；
        # R0-doc-intel 直构 Drone 绕过队列不计数；PoC/Devil 结果触发的重提案被
        # add_task 去重拦截（返回 None），不再 put、不计数。
        assert cerebrum._total_tasks == 3


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
