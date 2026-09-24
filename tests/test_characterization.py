"""可疑点表征测试（SPEC §8）。

registry #6（DockerSandbox 未接 PIPE）已修复转正：本文件现为其契约回归测试。
server/http.py 终态已删，registry #2 的 http 用例不再建立。
"""

import asyncio


class TestDockerSandboxPipes:
    """registry #6 已修复：DockerSandbox stdout/stderr 接 PIPE 的契约回归。"""

    async def test_docker_sandbox_captures_output(self, tmp_path, monkeypatch):
        from tools.poc_sandbox import DockerSandbox

        poc = tmp_path / "poc.py"
        poc.write_text("print('ok')", encoding="utf-8")

        captured: dict = {}

        class _FakeProc:
            returncode = 0
            pid = 12345

            async def communicate(self):
                return (b"ok\n", b"")  # 已接 PIPE → bytes

            async def wait(self):
                return 0

        async def fake_exec(*args, **kwargs):
            captured.update(kwargs)
            return _FakeProc()

        monkeypatch.setattr("asyncio.create_subprocess_exec", fake_exec)
        # _ensure_image 为同步方法，用同步替身避免真跑 docker
        monkeypatch.setattr(DockerSandbox, "_ensure_image", lambda self: None)
        result = await DockerSandbox().execute(poc, target="http://example.local")
        assert captured["stdout"] is asyncio.subprocess.PIPE
        assert captured["stderr"] is asyncio.subprocess.PIPE
        assert result.status.value == "verified_success"
        assert "ok" in result.output
