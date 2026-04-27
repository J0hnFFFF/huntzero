"""
server/core.py — KimiSecCore 统一控制器。

合并原有 HiveState（本地模式）+ ClusterController（集群模式）为单一类。
自动根据 redis_url 是否存在切换模式。

所有接口层（MCP / HTTP / CLI）统一调用此类，不再各自初始化引擎。
"""

import asyncio
import json
import logging
import os
import re
import shutil
import time
import uuid
from pathlib import Path
from typing import Any, Optional

logger = logging.getLogger("kimisec.core")

# ─── Redis Key 常量（与原有 kimi_cluster_mcp.py / kimi_worker.py 完全一致）────
QUEUE_KEY      = "kimisec:targets"
CLUSTER_KEY    = "kimisec:global:cluster"
FINDINGS_KEY   = "kimisec:global:findings"
EVENTS_CHANNEL = "kimisec:global:events"

ROOT_DIR = Path(__file__).parent.parent.resolve()


class KimiSecCore:
    """
    统一控制器 — 自动检测并切换 local / cluster 模式。

    local 模式：直接在进程内启动 Cerebrum（替代 kimi_mcp_server.py 的 HiveState）
    cluster 模式：操作 Redis 队列和全局状态（替代 kimi_cluster_mcp.py 的 ClusterController）
    """

    def __init__(
        self,
        redis_url: Optional[str] = None,
        work_dir: str = "./local_workspace",
    ):
        self.redis_url = redis_url
        self.mode = "cluster" if redis_url else "local"
        self.work_dir = Path(work_dir).resolve()
        self.work_dir.mkdir(parents=True, exist_ok=True)

        # Local mode state（替代 HiveState）
        self._blackboard = None
        self._cerebrum   = None
        self._task: Optional[asyncio.Task] = None

        # Redis sync client（lazy init）
        self._redis = None

    # ─────────────────────────────────────────────────────────────────────────
    #  Redis 访问
    # ─────────────────────────────────────────────────────────────────────────

    def _get_redis(self):
        """获取同步 Redis 客户端（lazy init）。"""
        if self._redis is None:
            if not self.redis_url:
                raise RuntimeError("Redis URL not configured. Use --redis or KIMI_REDIS_URL.")
            try:
                import redis
                self._redis = redis.from_url(self.redis_url, decode_responses=True)
            except ImportError:
                raise RuntimeError("pip install redis")
        return self._redis

    # ─────────────────────────────────────────────────────────────────────────
    #  队列管理（原 ClusterController.submit_targets / cancel_job）
    # ─────────────────────────────────────────────────────────────────────────

    def submit_targets(self, targets: list) -> str:
        """将一批目标压入 Redis 任务队列。"""
        r = self._get_redis()
        count = 0
        for item in targets:
            if isinstance(item, str):
                item = {"target": item}
            if "target" not in item:
                continue
            item.setdefault("job_id",            f"job-{uuid.uuid4().hex[:8]}")
            item.setdefault("max_workers",       5)
            item.setdefault("max_rounds",        30)
            item.setdefault("max_tasks",         200)
            item.setdefault("max_wall_time",     7200.0)
            item.setdefault("stagnation_rounds", 3)
            r.rpush(QUEUE_KEY, json.dumps(item))
            count += 1

        queue_len = r.llen(QUEUE_KEY)
        return (
            f"✅ Submitted {count} target(s) to queue.\n"
            f"   Queue depth: {queue_len} pending jobs.\n"
            f"   Each idle worker will pick up the next target automatically."
        )

    def get_queue_depth(self) -> int:
        return self._get_redis().llen(QUEUE_KEY)

    def cancel_job(self, job_id: str) -> str:
        r = self._get_redis()
        items = r.lrange(QUEUE_KEY, 0, -1)
        removed = 0
        for item in items:
            try:
                parsed = json.loads(item)
                if parsed.get("job_id") == job_id:
                    r.lrem(QUEUE_KEY, 0, item)
                    removed += 1
            except Exception:
                continue
        if removed:
            return f"✅ Removed {removed} queued task(s) with job_id '{job_id}'."
        return f"⚠️ job_id '{job_id}' not found in queue (may already be running)."

    # ─────────────────────────────────────────────────────────────────────────
    #  本地分析（原 HiveState）
    # ─────────────────────────────────────────────────────────────────────────

    @property
    def is_running(self) -> bool:
        return self._task is not None and not self._task.done()

    async def start_local(self, target: str, api_key: str, **kwargs) -> str:
        """在进程内启动 Cerebrum 分析（local 模式）。"""
        if self.is_running:
            return "⚠️  Analysis already in progress. Call stop first."

        from engine import Blackboard, Cerebrum

        max_workers       = int(kwargs.get("max_workers", 5))
        max_rounds        = int(kwargs.get("max_rounds", 30))
        max_tasks         = int(kwargs.get("max_tasks", 200))
        max_wall_time     = float(kwargs.get("max_wall_time", 7200.0))
        stagnation_rounds = int(kwargs.get("stagnation_rounds", 3))

        # 解析目标
        if re.match(r"https?://", target):
            analysis_dir = await self.prepare_git_target(target, self.work_dir)
            if not analysis_dir:
                return f"❌ Failed to clone target: {target}"
        else:
            analysis_dir = Path(target).resolve()
            if not analysis_dir.exists():
                return f"❌ Target path does not exist: {analysis_dir}"

        self._blackboard = Blackboard(self.work_dir)
        self._cerebrum = Cerebrum(
            blackboard=self._blackboard,
            work_dir=analysis_dir,
            root_dir=ROOT_DIR,
            config=self.build_config(api_key),
            max_concurrent_drones=max_workers,
            max_rounds=max_rounds,
            max_tasks=max_tasks,
            max_wall_time=max_wall_time,
            stagnation_rounds=stagnation_rounds,
        )

        self._task = asyncio.create_task(
            self._cerebrum.launch(str(analysis_dir))
        )

        return (
            f"✅ Hive-Mind launched.\n"
            f"   Target: {analysis_dir}\n"
            f"   Workers: {max_workers} | Max rounds: {max_rounds} | "
            f"Max tasks: {max_tasks}\n"
            f"   Call get_status() to monitor progress."
        )

    async def stop_local(self) -> str:
        if not self.is_running:
            return "ℹ️  No analysis is currently running."
        await self._cerebrum.stop("stop_requested")
        try:
            await asyncio.wait_for(self._task, timeout=10.0)
        except asyncio.TimeoutError:
            self._task.cancel()
        return "🛑 Analysis stopped."

    def local_status(self) -> dict:
        if self._blackboard is None:
            return {"active": False, "message": "No analysis has been started."}

        stats = self._blackboard.stats()
        snap  = self._blackboard.snapshot()
        hypotheses_summary = []
        for h in snap["hypotheses"].values():
            hypotheses_summary.append({
                "id":         h["id"],
                "status":     h["status"],
                "confidence": round(h["confidence"], 2),
                "claim":      h["description"][:100],
            })
        return {
            "active":         self.is_running,
            "target":         self._blackboard.target,
            "stats":          stats,
            "hypotheses":     hypotheses_summary,
            "findings_count": len(self._blackboard.findings),
        }

    def local_findings(self) -> list:
        if self._blackboard is None:
            return []
        result = []
        for f in self._blackboard.findings:
            result.append({
                "id":            f.id,
                "title":         f.title,
                "severity":      f.severity,
                "description":   f.description[:500],
                "evidence":      f.evidence[:300],
                "hypothesis_id": f.hypothesis_id,
            })
        return result

    # ─────────────────────────────────────────────────────────────────────────
    #  集群监控（原 ClusterController）
    # ─────────────────────────────────────────────────────────────────────────

    def get_cluster_dashboard(self) -> dict:
        r = self._get_redis()
        raw_nodes = r.hgetall(CLUSTER_KEY)
        nodes = []
        now   = time.time()
        active_count = 0

        for key, val in raw_nodes.items():
            try:
                data = json.loads(val)
                ts   = data.get("ts", 0)
                is_alive = (now - ts) < 120
                if data.get("active") and is_alive:
                    active_count += 1
                nodes.append({
                    "node_id":   key.replace(":heartbeat", ""),
                    "alive":     is_alive,
                    "active":    data.get("active", False),
                    "target":    data.get("target", ""),
                    "last_seen": f"{int(now - ts)}s ago",
                    "stats":     data.get("stats", {}),
                })
            except Exception:
                continue

        return {
            "total_nodes":   len(nodes),
            "active_jobs":   active_count,
            "queue_pending": self.get_queue_depth(),
            "nodes":         sorted(nodes, key=lambda x: x["alive"], reverse=True)[:50],
        }

    def query_global_findings(
        self,
        severity: Optional[str] = None,
        keyword:  Optional[str] = None,
        limit:    int = 50,
    ) -> list:
        r = self._get_redis()
        raw_list = r.zrevrange(FINDINGS_KEY, 0, limit * 3, withscores=False)

        results = []
        for raw in raw_list:
            try:
                f = json.loads(raw)
            except Exception:
                continue
            if severity and f.get("severity", "").lower() != severity.lower():
                continue
            if keyword and keyword.lower() not in json.dumps(f).lower():
                continue
            results.append(f)
            if len(results) >= limit:
                break
        return results

    def get_cross_project_insights(self) -> str:
        r        = self._get_redis()
        raw_list = r.zrevrange(FINDINGS_KEY, 0, 500, withscores=False)

        pattern_map: dict[str, list] = {}
        for raw in raw_list:
            try:
                f = json.loads(raw)
            except Exception:
                continue
            title_key = f.get("title", "").lower()[:50]
            sev       = f.get("severity", "")
            if sev in ("critical", "high"):
                pattern_map.setdefault(title_key, []).append(f)

        lines = ["## 🔎 Cross-Project Pattern Analysis\n"]
        suspicious = {k: v for k, v in pattern_map.items() if len(v) >= 2}
        if not suspicious:
            lines.append("No recurring high-severity patterns found yet.")
        else:
            for pattern, findings in sorted(suspicious.items(), key=lambda x: -len(x[1])):
                lines.append(f"### 🚨 `{pattern.title()}` — appears in **{len(findings)} projects**")
                lines.append(f"Severity: {findings[0].get('severity', '?').upper()}")
                lines.append("Projects affected:")
                for f in findings[:5]:
                    lines.append(f"  - Finding ID: `{f.get('id', '')}` | Hypothesis: `{f.get('hypothesis_id', '')}`")
                lines.append("")
        return "\n".join(lines)

    # ─────────────────────────────────────────────────────────────────────────
    #  统一 API（自动路由到 local / cluster）
    # ─────────────────────────────────────────────────────────────────────────

    async def submit(self, targets: list, api_key: str = "") -> str:
        if self.mode == "cluster":
            return self.submit_targets(targets)
        # local 模式：直接运行第一个目标
        if not targets:
            return "❌ No targets provided."
        first = targets[0]
        target = first if isinstance(first, str) else first.get("target", "")
        opts = first if isinstance(first, dict) else {}
        if not api_key:
            return "❌ API key required for local mode."
        return await self.start_local(target, api_key, **opts)

    def get_status(self) -> dict:
        if self.mode == "cluster":
            return self.get_cluster_dashboard()
        return self.local_status()

    def get_findings(
        self,
        severity: Optional[str] = None,
        keyword:  Optional[str] = None,
        limit:    int = 50,
    ) -> list:
        if self.mode == "cluster":
            return self.query_global_findings(severity, keyword, limit)
        findings = self.local_findings()
        if severity:
            findings = [f for f in findings if f.get("severity", "").lower() == severity.lower()]
        if keyword:
            findings = [f for f in findings if keyword.lower() in json.dumps(f).lower()]
        return findings[:limit]

    # ─────────────────────────────────────────────────────────────────────────
    #  共享工具函数（原 kimi_hive.py 的 build_config / prepare_git_target）
    # ─────────────────────────────────────────────────────────────────────────

    @staticmethod
    def build_config(api_key: str):
        """构建 kimi-agent-sdk Config（与 kimi_hive.build_config 完全一致）。"""
        from kimi_agent_sdk import Config
        base_url = os.environ.get("KIMI_BASE_URL", "https://api.kimi.com/coding/v1")
        return Config(
            default_model="kimi-for-coding",
            providers={"kimi": {
                "type":     "kimi",
                "base_url": base_url,
                "api_key":  api_key,
                "timeout":  3600.0,
            }},
            models={"kimi-for-coding": {
                "provider":         "kimi",
                "model":            "kimi-for-coding",
                "max_context_size": 262144,
            }},
        )

    @staticmethod
    async def prepare_git_target(url: str, work_dir: Path) -> Optional[Path]:
        """克隆 Git 仓库（与 kimi_hive.prepare_git_target 逻辑一致，日志用 logging）。"""
        repo_name  = url.rstrip("/").split("/")[-1].replace(".git", "")
        projects   = work_dir / "projects"
        projects.mkdir(parents=True, exist_ok=True)
        target_dir = projects / repo_name

        logger.info("Cloning %s → %s", url, target_dir)

        if not shutil.which("git"):
            logger.error("git not found in PATH")
            return None

        if target_dir.exists():
            cmd = ["git", "-C", str(target_dir), "pull"]
        else:
            cmd = ["git", "clone", "--depth", "1", url, str(target_dir)]

        proc = await asyncio.create_subprocess_exec(
            *cmd,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )
        _, stderr_data = await proc.communicate()
        if proc.returncode != 0:
            logger.error("Git error: %s", stderr_data.decode(errors="ignore").strip())
            return None

        logger.info("Ready → %s", target_dir)
        return target_dir
