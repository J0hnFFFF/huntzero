"""fake-LLM 集成测试 harness（SPEC 5.3）。

用法：
    monkeypatch.setattr(kimi_agent_sdk.Session, "create", make_fake_create(script))
script(prompt: str) -> str：按 prompt 内容路由到 cerebrum/drone/critic 预设输出。
"""

from __future__ import annotations

import json
from collections.abc import Callable
from typing import Any

from kimi_agent_sdk import TextPart

# 注意：不可带 trace_needed 行——"no" 会被 _parse_round_output 解析为真值字符串，
# _evaluate_chase_decision 据此进入 chase 轮，chase prompt 不含 "[DRONE TASK:"
# 会被路由到 cerebrum 分支，last_raw 被覆盖后 finding 丢失。
DRONE_REPORT = """\
finding: Buffer overflow in parse_header via unchecked length prefix
severity: high
confidence: 0.9
evidence: parse_header copies length-prefixed token into 256-byte stack buffer without bounds check (app.py:12)
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
    {
        "thinking": "audit complete",
        "hypotheses": [],
        "tasks": [],
        "is_complete": True,
        "complete_reason": "done",
    }
)

EMPTY_ROUND = json.dumps(
    {"thinking": "waiting for drone results", "hypotheses": [], "tasks": [], "is_complete": False}
)


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
