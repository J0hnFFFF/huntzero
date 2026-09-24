"""
engine/backends.py — Blackboard 可插拔存储后端协议层。

定义了一个具体实现：
  LocalBackend   — 本地 .blackboard.json，向后兼容。

使用方式：
  from engine.backends import LocalBackend

  backend = LocalBackend(Path("./local_workspace"))
"""

from __future__ import annotations

import json
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
        """向全局事件总线发布遥测事件（可选实现，本地模式为 no-op）。"""

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
        """原子写入完整快照：先写临时文件再重命名，避免写入中断导致数据损坏。"""
        path = self.work_dir / ".blackboard.json"
        tmp_path = self.work_dir / ".blackboard.json.tmp"
        try:
            tmp_path.write_text(
                json.dumps(snapshot, ensure_ascii=False, indent=2, default=str),
                encoding="utf-8",
            )
            tmp_path.replace(path)
        except OSError as exc:
            # 静默丢弃但记录日志，避免磁盘满/权限问题时阻塞主流程
            import logging
            logging.getLogger(__name__).warning(f"Blackboard persist failed: {exc}")

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
