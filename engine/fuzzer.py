import asyncio
import logging
from pathlib import Path
from typing import Dict, Any, Optional

logger = logging.getLogger(__name__)

class FuzzJob:
    """
    Manages a single fuzzing job executed inside an isolated Docker container.
    """
    def __init__(self, job_dir: Path, target_src: Path, timeout_minutes: int = 15):
        self.job_dir = job_dir
        self.target_src = target_src
        self.timeout_minutes = timeout_minutes

    def _parse_asan_log(self, log_path: Path) -> str:
        """Extracts the AddressSanitizer/UBSan crash log."""
        if not log_path.exists():
            return ""
        try:
            content = log_path.read_text(encoding="utf-8", errors="replace")
            # Return up to 4000 characters of the crash log to avoid context bloat
            if len(content) > 4000:
                return content[:4000] + "\n...[truncated ASAN log]"
            return content
        except Exception as e:
            return f"Failed to read ASAN log: {e}"

    async def run(self) -> Dict[str, Any]:
        """
        Executes the fuzzing harness inside the Docker container.
        """
        # Ensure build.sh is executable
        build_sh = self.job_dir / "build.sh"
        if not build_sh.exists():
            return {"error": "build.sh not found in job directory."}
            
        # Create output directories for the fuzzer to use
        crashes_dir = self.job_dir / "crashes"
        crashes_dir.mkdir(parents=True, exist_ok=True)
        corpus_dir = self.job_dir / "corpus"
        corpus_dir.mkdir(parents=True, exist_ok=True)
        
        # We will mount the job_dir and target_src into the docker container.
        # Ensure docker image is built (in a real setup, this would be assured beforehand).
        cmd = [
            "docker", "run", "--rm",
            "--network=none",                      # Network isolation
            "--memory=4g",                         # Memory limit
            "--cpus=2",                            # CPU limit
            "-v", f"{self.job_dir.absolute()}:/fuzz/job:rw",
            "-v", f"{self.target_src.absolute()}:/fuzz/src:ro", # Target source mounted readonly
            "kimisec-fuzzer:latest",
            "/bin/bash", "-c", "cd /fuzz/job && chmod +x build.sh && ./build.sh && if [ -f ./fuzzer ]; then ASAN_OPTIONS=abort_on_error=1,symbolize=1 ./fuzzer -max_total_time=${TIMEOUT} -artifact_prefix=/fuzz/job/crashes/ /fuzz/job/corpus/ > /fuzz/job/asan_log.txt 2>&1; fi"
        ]
        
        # Replace ${TIMEOUT} with seconds
        cmd[-1] = cmd[-1].replace("${TIMEOUT}", str(self.timeout_minutes * 60))

        logger.info(f"Starting fuzz job in: {self.job_dir}")
        proc = await asyncio.create_subprocess_exec(
            *cmd,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )

        try:
            # We add a buffer of +60s for docker spinup and teardown
            stdout, stderr = await asyncio.wait_for(
                proc.communicate(), 
                timeout=(self.timeout_minutes * 60) + 60
            )
        except asyncio.TimeoutError:
            # If the strict timeout hits, kill the docker proc
            logger.warning(f"Fuzz job {self.job_dir} timed out on host.")
            try:
                proc.kill()
                await proc.communicate()
            except Exception:
                pass
            return {"error": "Fuzz job execution timed out globally."}

        # Collect crash samples
        crashes = list(crashes_dir.glob("crash-*"))
        asan_log = self._parse_asan_log(self.job_dir / "asan_log.txt")

        return {
            "exit_code": proc.returncode,
            "crashes": len(crashes),
            "crash_files": [str(c.name) for c in crashes],
            "asan_log": asan_log,
        }
