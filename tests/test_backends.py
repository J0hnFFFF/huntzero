"""engine/backends.py 存储后端（SPEC 5.3）。

终态仅余 LocalBackend（RedisBackend/IncrementalLocalBackend/make_backend
已随集群模式删除），故只保留 LocalBackend 用例。
"""

from engine.backends import LocalBackend


def _snapshot(findings: int = 1) -> dict:
    return {
        "target": "proj",
        "active": True,
        "hypotheses": {"H-1": {"id": "H-1", "confidence": 0.6}},
        "tasks": {},
        "findings": [
            {"id": f"F-{i}", "title": f"t{i}", "severity": "high", "created_at": float(i)}
            for i in range(findings)
        ],
    }


class TestLocalBackend:
    def test_atomic_save_and_load(self, tmp_path):
        be = LocalBackend(tmp_path)
        be.save("n1", _snapshot())
        assert (tmp_path / ".blackboard.json").exists()
        assert not (tmp_path / ".blackboard.json.tmp").exists()  # rename 完成
        loaded = be.load("n1")
        assert loaded["target"] == "proj"

    def test_load_missing_returns_none(self, tmp_path):
        assert LocalBackend(tmp_path).load("nope") is None

    def test_load_corrupted_returns_none(self, tmp_path):
        (tmp_path / ".blackboard.json").write_text("{bad", encoding="utf-8")
        assert LocalBackend(tmp_path).load("n1") is None
