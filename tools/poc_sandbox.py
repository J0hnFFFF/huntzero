"""
tools/poc_sandbox.py — PoC 沙箱执行器。

支持两种模式：
1. local: 本地受限执行（timeout + 资源限制）
2. docker: Docker 容器隔离执行（推荐）

Usage:
    python tools/poc_sandbox.py execute --poc poc.py --target http://target:8080
    python tools/poc_sandbox.py execute --poc poc.py --target http://target:8080 --mode docker
"""

from __future__ import annotations

import argparse
import asyncio
import json
import os
import subprocess
import tempfile
import time
import uuid
from dataclasses import dataclass, asdict
from enum import Enum
from pathlib import Path
from typing import Optional


class PoCStatus(str, Enum):
    PENDING = "pending"
    GENERATED = "generated"
    VERIFIED_SUCCESS = "verified_success"
    VERIFIED_FAILED = "verified_failed"
    ERROR = "error"
    TIMEOUT = "timeout"


@dataclass
class PoCResult:
    status: PoCStatus
    output: str
    error: Optional[str] = None
    duration_ms: int = 0
    executed_at: Optional[float] = None


class LocalSandbox:
    """本地受限执行环境（轻量级，适合快速验证）"""

    def __init__(self, timeout: int = 30, max_output: int = 10000):
        self.timeout = timeout
        self.max_output = max_output

    async def execute(self, poc_path: Path, target: str, **kwargs) -> PoCResult:
        """执行 PoC 脚本（Python/Bash）"""
        start = time.monotonic()

        if not poc_path.exists():
            return PoCResult(
                status=PoCStatus.ERROR,
                output="",
                error=f"PoC file not found: {poc_path}",
                duration_ms=int((time.monotonic() - start) * 1000),
            )

        # 根据文件扩展名选择解释器
        ext = poc_path.suffix.lower()
        if ext == ".py":
            cmd = ["python", str(poc_path), "--target", target]
        elif ext in (".sh", ".bash"):
            cmd = ["bash", str(poc_path), "--target", target]
        else:
            return PoCResult(
                status=PoCStatus.ERROR,
                output="",
                error=f"Unsupported file type: {ext}",
                duration_ms=int((time.monotonic() - start) * 1000),
            )

        # 注入环境变量（供 PoC 使用）
        env = os.environ.copy()
        env["POC_TARGET"] = target
        env["POC_ID"] = str(uuid.uuid4())[:8]

        try:
            proc = await asyncio.create_subprocess_exec(
                *cmd,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
                env=env,
                limit=self.max_output,
            )
            try:
                stdout, stderr = await asyncio.wait_for(
                    proc.communicate(), timeout=self.timeout
                )
            except asyncio.TimeoutError:
                proc.kill()
                return PoCResult(
                    status=PoCStatus.TIMEOUT,
                    output="",
                    error=f"PoC execution timed out after {self.timeout}s",
                    duration_ms=int((time.monotonic() - start) * 1000),
                )

            output = stdout.decode(errors="replace")[: self.max_output]
            if stderr:
                output += "\n[STDERR]:\n" + stderr.decode(errors="replace")[: self.max_output // 2]

            if proc.returncode == 0:
                status = PoCStatus.VERIFIED_SUCCESS
            else:
                status = PoCStatus.VERIFIED_FAILED

            return PoCResult(
                status=status,
                output=output,
                duration_ms=int((time.monotonic() - start) * 1000),
                executed_at=time.time(),
            )

        except Exception as e:
            return PoCResult(
                status=PoCStatus.ERROR,
                output="",
                error=str(e),
                duration_ms=int((time.monotonic() - start) * 1000),
            )


class DockerSandbox:
    """Docker 容器隔离执行（生产级安全隔离）"""

    def __init__(self, image: str = "python:3.11-slim", timeout: int = 60):
        self.image = image
        self.timeout = timeout

    def _ensure_image(self):
        """确保 Docker 镜像存在（懒加载）"""
        try:
            result = subprocess.run(
                ["docker", "image", "inspect", self.image],
                capture_output=True,
                timeout=10,
            )
            if result.returncode != 0:
                print(f"[DockerSandbox] Pulling image: {self.image}")
                subprocess.run(
                    ["docker", "pull", self.image],
                    capture_output=True,
                    timeout=300,
                )
        except Exception as e:
            print(f"[DockerSandbox] Warning: {e}")

    async def execute(self, poc_path: Path, target: str, **kwargs) -> PoCResult:
        """在 Docker 容器中执行 PoC"""
        start = time.monotonic()

        if not poc_path.exists():
            return PoCResult(
                status=PoCStatus.ERROR,
                output="",
                error=f"PoC file not found: {poc_path}",
                duration_ms=int((time.monotonic() - start) * 1000),
            )

        self._ensure_image()

        # 临时目录用于挂载
        with tempfile.TemporaryDirectory() as tmpdir:
            poc_copy = Path(tmpdir) / poc_path.name
            poc_copy.write_bytes(poc_path.read_bytes())

            ext = poc_path.suffix.lower()
            if ext == ".py":
                cmd = ["docker", "run", "--rm", "--network=none", "-v", f"{tmpdir}:/workspace", "-w", "/workspace", self.image, "python", "/workspace/" + poc_path.name, "--target", target]
            elif ext in (".sh", ".bash"):
                cmd = ["docker", "run", "--rm", "--network=none", "-v", f"{tmpdir}:/workspace", "-w", "/workspace", self.image, "bash", "/workspace/" + poc_path.name, "--target", target]
            else:
                return PoCResult(
                    status=PoCStatus.ERROR,
                    output="",
                    error=f"Unsupported file type: {ext}",
                    duration_ms=int((time.monotonic() - start) * 1000),
                )

            env = os.environ.copy()
            env["POC_TARGET"] = target
            env["POC_ID"] = str(uuid.uuid4())[:8]

            try:
                proc = await asyncio.create_subprocess_exec(
                    *cmd, env=env
                )
                try:
                    stdout, stderr = await asyncio.wait_for(
                        proc.communicate(), timeout=self.timeout
                    )
                except asyncio.TimeoutError:
                    subprocess.run(["docker", "kill", str(proc.pid)], capture_output=True)
                    return PoCResult(
                        status=PoCStatus.TIMEOUT,
                        output="",
                        error=f"PoC execution timed out after {self.timeout}s",
                        duration_ms=int((time.monotonic() - start) * 1000),
                    )

                output = stdout.decode(errors="replace")[: 10000]
                if stderr:
                    output += "\n[STDERR]:\n" + stderr.decode(errors="replace")[: 5000]

                if proc.returncode == 0:
                    status = PoCStatus.VERIFIED_SUCCESS
                else:
                    status = PoCStatus.VERIFIED_FAILED

                return PoCResult(
                    status=status,
                    output=output,
                    duration_ms=int((time.monotonic() - start) * 1000),
                    executed_at=time.time(),
                )

            except Exception as e:
                return PoCResult(
                    status=PoCStatus.ERROR,
                    output="",
                    error=str(e),
                    duration_ms=int((time.monotonic() - start) * 1000),
                )


async def execute_poc(poc_path: str, target: str, mode: str = "local", **kwargs) -> dict:
    """
    主入口：根据 mode 选择沙箱执行 PoC。
    返回结构化结果（供 Blackboard.add_finding() 调用）。
    """
    poc_file = Path(poc_path)
    if mode == "docker":
        sandbox = DockerSandbox(timeout=kwargs.get("timeout", 60))
    else:
        sandbox = LocalSandbox(timeout=kwargs.get("timeout", 30))

    result = await sandbox.execute(poc_file, target)

    return {
        "poc_status": result.status.value,
        "poc_output": result.output,
        "poc_error": result.error,
        "poc_duration_ms": result.duration_ms,
        "poc_verified_at": result.executed_at,
    }


# ─────────────────────────────────────────────────────────────────────────────
#  CLI 入口
# ─────────────────────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(
        prog="poc_sandbox",
        description="PoC sandbox executor for vulnerability verification",
    )
    sub = parser.add_subparsers(dest="command", required=True)

    # execute
    p1 = sub.add_parser("execute", help="Execute a PoC script")
    p1.add_argument("--poc", "-p", required=True, help="Path to PoC script")
    p1.add_argument("--target", "-t", required=True, help="Target URL or endpoint")
    p1.add_argument("--mode", "-m", default="local", choices=["local", "docker"],
                    help="Execution mode (default: local)")
    p1.add_argument("--timeout", type=int, default=30,
                    help="Timeout in seconds (default: 30)")
    p1.set_defaults(func=lambda a: asyncio.run(execute_poc(
        a.poc, a.target, mode=a.mode, timeout=a.timeout
    )))

    # check
    p2 = sub.add_parser("check", help="Check sandbox prerequisites")
    p2.add_argument("--mode", "-m", default="local", choices=["local", "docker"])
    p2.set_defaults(func=lambda a: print(json.dumps(_check_prerequisites(a.mode), indent=2)))

    args = parser.parse_args()
    result = args.func(args)
    if result:
        print(json.dumps(result, indent=2, ensure_ascii=False, default=str))


def _check_prerequisites(mode: str) -> dict:
    """检查沙箱前置条件"""
    if mode == "docker":
        try:
            r = subprocess.run(["docker", "--version"], capture_output=True, timeout=5)
            docker_ok = r.returncode == 0
        except Exception:
            docker_ok = False
        return {"mode": "docker", "docker_available": docker_ok}
    return {"mode": "local", "ready": True}


if __name__ == "__main__":
    main()
