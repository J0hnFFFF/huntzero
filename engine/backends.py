"""
engine/backends.py — Blackboard 可插拔存储后端协议层。

定义了两个具体实现：
  LocalBackend   — 现有行为（本地 .blackboard.json），向后兼容。
  RedisBackend   — 将 Snapshot 序列化到 Redis，同时通过 Pub/Sub
                   广播状态变更事件，实现跨节点的实时同步。

使用方式：
  from engine.backends import make_backend

  # 本地（默认，无需依赖）
  backend = make_backend("local://./local_workspace")

  # Redis（集群分布式模式）
  backend = make_backend("redis://localhost:6379/0?node=node-uuid&target=uid")
"""

from __future__ import annotations

import asyncio
import json
import time
from abc import ABC, abstractmethod
from pathlib import Path
from typing import Any, Optional


# ─────────────────────────────────────────────────────────────────────────────
#  Backend Protocol
# ─────────────────────────────────────────────────────────────────────────────

class StorageBackend(ABC):
    """所有存储后端必须实现的最小接口。"""

    @abstractmethod
    def save(self, node_id: str, snapshot: dict) -> None:
        """将快照写入持久化存储。node_id 是运行中节点的唯一 ID。"""

    @abstractmethod
    def load(self, node_id: str) -> Optional[dict]:
        """从存储读取快照，若不存在返回 None。"""

    @abstractmethod
    def emit_event(self, node_id: str, event_type: str, data: Any) -> None:
        """向全局事件总线发布遥测事件（可选实现，无 Redis 时为 no-op）。"""

    @abstractmethod
    def write_audit_notes(self, node_id: str, content: str) -> None:
        """将人类可读的审计摘要写到可见位置。"""


# ─────────────────────────────────────────────────────────────────────────────
#  Local Backend（现有行为，零额外依赖，向后兼容）
# ─────────────────────────────────────────────────────────────────────────────

class LocalBackend(StorageBackend):
    """
    将 Blackboard 状态写到本地工作目录的 .blackboard.json 文件。
    与当前 v7.x 行为完全等价，不引入任何新依赖。
    """

    def __init__(self, work_dir: Path):
        self.work_dir = work_dir
        work_dir.mkdir(parents=True, exist_ok=True)

    def save(self, node_id: str, snapshot: dict) -> None:
        path = self.work_dir / ".blackboard.json"
        try:
            path.write_text(
                json.dumps(snapshot, ensure_ascii=False, indent=2, default=str),
                encoding="utf-8",
            )
        except OSError:
            pass

    def load(self, node_id: str) -> Optional[dict]:
        path = self.work_dir / ".blackboard.json"
        if not path.exists():
            return None
        try:
            return json.loads(path.read_text(encoding="utf-8"))
        except Exception:
            return None

    def emit_event(self, node_id: str, event_type: str, data: Any) -> None:
        pass  # 本地模式无全局总线，no-op

    def write_audit_notes(self, node_id: str, content: str) -> None:
        try:
            (self.work_dir / ".audit_notes.md").write_text(content, encoding="utf-8")
        except OSError:
            pass


# ─────────────────────────────────────────────────────────────────────────────
#  Redis Backend（分布式集群模式）
# ─────────────────────────────────────────────────────────────────────────────

class RedisBackend(StorageBackend):
    """
    将 Blackboard 快照序列化到 Redis Hash，
    并通过 Pub/Sub Channel 向全局总线广播遥测事件。

    Redis Key 规范：
      kimisec:node:{node_id}:snapshot    – Hash，存完整状态快照
      kimisec:node:{node_id}:audit       – String，审计摘要 Markdown
      kimisec:global:events              – Pub/Sub Channel，遥测事件流
      kimisec:global:findings            – Sorted Set，leaderboard（全局发现）

    依赖：pip install redis
    """

    SNAPSHOT_TTL = 3600 * 24 * 7   # 7 天后自动过期（防止僵尸节点残留）

    def __init__(self, redis_url: str, node_id: str):
        try:
            import redis as redis_lib
            self._r = redis_lib.from_url(redis_url, decode_responses=True)
        except ImportError:
            raise ImportError(
                "Redis backend requires 'redis' package. "
                "Install it with: pip install redis"
            )
        self._node_id   = node_id
        self._redis_url = redis_url

    def _snapshot_key(self) -> str:
        return f"kimisec:node:{self._node_id}:snapshot"

    def _audit_key(self) -> str:
        return f"kimisec:node:{self._node_id}:audit"

    def save(self, node_id: str, snapshot: dict) -> None:
        try:
            serialized = json.dumps(snapshot, ensure_ascii=False, default=str)
            pipe = self._r.pipeline()
            pipe.set(self._snapshot_key(), serialized)
            pipe.expire(self._snapshot_key(), self.SNAPSHOT_TTL)
            # 同时更新全局 Findings Leaderboard（Sorted Set，score=时间戳）
            for f in snapshot.get("findings", []):
                finding_json = json.dumps(f, ensure_ascii=False)
                pipe.zadd(
                    "kimisec:global:findings",
                    {finding_json: f.get("created_at", time.time())}
                )
            # 更新节点心跳（让 OpenClaw 能知道节点还活着）
            pipe.hset(
                "kimisec:global:cluster",
                f"{node_id}:heartbeat",
                json.dumps({
                    "ts":      time.time(),
                    "target":  snapshot.get("target", ""),
                    "active":  snapshot.get("active", False),
                    "stats": {
                        "hypotheses": len(snapshot.get("hypotheses", {})),
                        "findings":   len(snapshot.get("findings", [])),
                    }
                })
            )
            pipe.execute()
        except Exception:
            pass

    def load(self, node_id: str) -> Optional[dict]:
        try:
            raw = self._r.get(self._snapshot_key())
            if raw:
                return json.loads(raw)
        except Exception:
            pass
        return None

    def emit_event(self, node_id: str, event_type: str, data: Any) -> None:
        """向全局 Pub/Sub channel 广播遥测事件，供 OpenClaw / 监控面板消费。"""
        try:
            msg = json.dumps({
                "type":    event_type,   # 前端用 msg.type 读取
                "node_id": node_id,
                "data":    data,
                "ts":      time.time(),
            }, ensure_ascii=False, default=str)
        except Exception:
            return

        try:
            loop = asyncio.get_running_loop()
        except RuntimeError:
            loop = None

        if loop is not None and loop.is_running():
            # 异步上下文：用 aioredis 失火就忘式 publish
            # ensure_future 把协程注入当前 running loop，不需要 await
            redis_url = self._redis_url

            async def _async_pub():
                try:
                    import redis.asyncio as _aioredis
                    r = _aioredis.from_url(redis_url, decode_responses=True)
                    await r.publish("kimisec:global:events", msg)
                    await r.aclose()
                except Exception:
                    pass

            asyncio.ensure_future(_async_pub())
        else:
            # 纯同步上下文（单机本地测试等）：直接调用同步客户端
            try:
                self._r.publish("kimisec:global:events", msg)
            except Exception:
                pass

    def write_audit_notes(self, node_id: str, content: str) -> None:
        try:
            pipe = self._r.pipeline()
            pipe.set(self._audit_key(), content)
            pipe.expire(self._audit_key(), self.SNAPSHOT_TTL)
            pipe.execute()
        except Exception:
            pass
        
        # 额外在宿主机/挂载卷中保存一份物理 Markdown 文件
        import os
        from pathlib import Path
        reports_dir = os.environ.get("KIMI_REPORTS_DIR", "/data/reports")
        rd = Path(reports_dir)
        try:
            if rd.exists() or os.environ.get("KIMI_REPORTS_DIR"):
                rd.mkdir(parents=True, exist_ok=True)
                (rd / f"{node_id}_audit_notes.md").write_text(content, encoding="utf-8")
        except Exception:
            pass


# ─────────────────────────────────────────────────────────────────────────────
#  工厂函数
# ─────────────────────────────────────────────────────────────────────────────

def make_backend(dsn: str, node_id: str = "local", work_dir: Optional[Path] = None) -> StorageBackend:
    """
    根据 DSN 字符串选择并创建 StorageBackend。

    示例：
        make_backend("local://./workspace")          → LocalBackend
        make_backend("redis://localhost:6379/0", node_id="abc123")  → RedisBackend
    """
    if dsn.startswith("redis://") or dsn.startswith("rediss://"):
        return RedisBackend(redis_url=dsn, node_id=node_id)

    # 默认 local 模式
    if work_dir is None:
        # 从 DSN 里解析路径，如 "local://./workspace" → "./workspace"
        path_str = dsn.replace("local://", "").strip("/")
        work_dir = Path(path_str) if path_str else Path("./local_workspace")
    return LocalBackend(work_dir=work_dir)
