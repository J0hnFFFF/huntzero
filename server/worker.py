"""
server/worker.py — Redis BLPOP Worker 消费循环。

替代原有 kimi_worker.py，使用 KimiSecCore 的共享工具函数。
所有 Redis Key 名称、心跳格式、Job JSON 格式与原有完全一致。
"""

import asyncio
import json
import logging
import re
import socket
import time
import uuid
from pathlib import Path
from typing import Optional

import redis.asyncio as aioredis

from .core import KimiSecCore, QUEUE_KEY, CLUSTER_KEY

logger = logging.getLogger("kimisec.worker")


async def write_heartbeat(
    r: aioredis.Redis,
    node_id: str,
    *,
    active: bool,
    target: str = "",
    stats: Optional[dict] = None,
) -> None:
    """向 Redis Hash 写入心跳记录（与 kimi_worker.py 完全一致）。"""
    try:
        payload = json.dumps({
            "ts":     time.time(),
            "active": active,
            "target": target,
            "stats":  stats or {},
        })
        await r.hset(CLUSTER_KEY, f"{node_id}:heartbeat", payload)
    except Exception:
        pass


async def run_one_job(job: dict, redis_url: str, api_key: str, work_root: Path):
    """针对单个目标启动完整 Hive-Mind 分析（与 kimi_worker.run_one_job 逻辑一致）。"""
    from engine import Blackboard, Cerebrum
    from engine.backends import make_backend

    job_id            = job.get("job_id", uuid.uuid4().hex)
    target            = job["target"]
    max_workers       = int(job.get("max_workers", 5))
    max_rounds        = int(job.get("max_rounds", 30))
    max_tasks         = int(job.get("max_tasks", 200))
    max_wall_time     = float(job.get("max_wall_time", 7200.0))
    stagnation_rounds = int(job.get("stagnation_rounds", 3))

    logger.info("[%s] Starting analysis → %s", job_id, target)

    # 心跳
    _hb_r = aioredis.from_url(redis_url, decode_responses=True)

    async def _heartbeat_loop():
        while True:
            await write_heartbeat(_hb_r, job_id, active=True, target=target)
            await asyncio.sleep(30)

    hb_task = asyncio.create_task(_heartbeat_loop())
    await write_heartbeat(_hb_r, job_id, active=True, target=target)

    # 工作目录
    work_dir = work_root / job_id
    work_dir.mkdir(parents=True, exist_ok=True)

    root_dir = Path(__file__).parent.parent.resolve()

    # 解析目标
    if re.match(r"https?://", target):
        analysis_dir = await KimiSecCore.prepare_git_target(target, work_dir)
        if not analysis_dir:
            logger.error("[%s] Failed to clone: %s", job_id, target)
            hb_task.cancel()
            await _hb_r.aclose()
            return
    else:
        analysis_dir = Path(target)
        if not analysis_dir.exists():
            logger.error("[%s] Path not found: %s", job_id, analysis_dir)
            hb_task.cancel()
            await _hb_r.aclose()
            return

    # 初始化（Redis 后端）
    backend    = make_backend(redis_url, node_id=job_id, work_dir=work_dir)
    blackboard = Blackboard(work_dir=work_dir, backend=backend, node_id=job_id)

    cerebrum = Cerebrum(
        blackboard=blackboard,
        work_dir=analysis_dir,
        root_dir=root_dir,
        config=KimiSecCore.build_config(api_key),
        max_concurrent_drones=max_workers,
        max_rounds=max_rounds,
        max_tasks=max_tasks,
        max_wall_time=max_wall_time,
        stagnation_rounds=stagnation_rounds,
    )

    try:
        await cerebrum.launch(str(analysis_dir))
        blackboard.emit_global("job_completed", {
            "job_id":   job_id,
            "target":   target,
            "findings": len(blackboard.findings),
        })
        logger.info("[%s] Done. Findings: %d", job_id, len(blackboard.findings))
    except Exception as e:
        logger.exception("[%s] Fatal error: %s", job_id, e)
        blackboard.emit_global("job_failed", {"job_id": job_id, "error": str(e)})
    finally:
        hb_task.cancel()
        await write_heartbeat(_hb_r, job_id, active=False, target="", stats={
            "hypotheses": len(blackboard.hypotheses),
            "findings":   len(blackboard.findings),
        })
        await _hb_r.aclose()


async def worker_loop(redis_url: str, api_key: str, work_root: Path):
    """主循环：无限阻塞从 Redis 队列拉取任务并执行。"""
    r = aioredis.from_url(redis_url, decode_responses=True)
    node_id = f"{socket.gethostname()}-{uuid.uuid4().hex[:6]}"
    logger.info("Worker ready. Node ID: %s | Listening on %s @ %s", node_id, QUEUE_KEY, redis_url)

    while True:
        try:
            await write_heartbeat(r, node_id, active=False)
            result = await r.blpop(QUEUE_KEY, timeout=60)
            if result is None:
                continue

            _, payload = result
            try:
                job = json.loads(payload)
            except json.JSONDecodeError:
                logger.warning("Invalid job payload: %s", payload[:200])
                continue

            await run_one_job(job, redis_url, api_key, work_root)

        except asyncio.CancelledError:
            break
        except KeyboardInterrupt:
            break
        except Exception as e:
            logger.exception("Worker loop error: %s", e)
            await asyncio.sleep(5)

    logger.info("Worker shutting down.")
    await r.aclose()
