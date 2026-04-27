"""
Sector — 大型项目分区攻击引擎。

核心思想：
  不要用一个大脑分析整个项目，而是用 N 个专注的小脑各自分析
  一个「分区(Sector)」，再由协调者(Coordinator)汇聚发现。

  人类安全团队也是这么做的：
  - 研究员 A 负责认证模块
  - 研究员 B 负责 API 层
  - 研究员 C 负责文件处理
  - Team Lead 看所有人的发现，识别跨模块利用链

架构层级：
  Project → SectorManager.decompose() → [Sector₁, Sector₂, ..., Sectorₙ]
  每个 Sector 拥有独立的：
    - BlackboardPartition（隔离的假设/任务空间，Finding 自动汇聚到主 Blackboard）
    - Mini-Cerebrum 循环（独立 LLM Session，独立上下文窗口）
    - 收窄的 work_dir（只看 Sector 对应的子目录）
"""
import asyncio
import os
import re
import time
import uuid
from dataclasses import dataclass, field, asdict
from pathlib import Path
from typing import Any, Optional

from .blackboard import (
    Blackboard,
    BlackboardPartition,
    HypothesisStatus,
    TaskStatus,
    HypothesisNode,
    DroneTask,
    Finding,
)
from .drone import Drone


# ─────────────────────────────────────────────────────────────────────────────
#  Sector 数据结构
# ─────────────────────────────────────────────────────────────────────────────

@dataclass
class Sector:
    """一个分析分区 — 项目中一个内聚的子系统。"""

    id: str                     # "sector-auth", "sector-api", ...
    name: str                   # 人类可读名称: "Authentication Module"
    path: str                   # 相对于项目根的路径: "src/auth"
    description: str            # 该子系统的功能描述
    attack_surface: str         # 攻击面描述: "HTTP endpoints with session handling"
    priority: int               # 0=最高优先级
    estimated_files: int = 0    # 预估文件数
    status: str = "pending"     # pending / running / done / failed
    findings_count: int = 0     # 该 Sector 产出的 Finding 数量
    started_at: Optional[float] = None
    completed_at: Optional[float] = None
    error: Optional[str] = None


# ─────────────────────────────────────────────────────────────────────────────
#  SectorManager — 项目分解与分区调度
# ─────────────────────────────────────────────────────────────────────────────

# 大型项目阈值（与 cerebrum.py 中 _strategic_recon 保持一致）
LARGE_PROJECT_FILE_THRESHOLD = 5000
LARGE_PROJECT_SIZE_THRESHOLD = 50_000_000  # 50 MB

# 分解后单个 Sector 的理想文件数范围
SECTOR_IDEAL_MIN_FILES = 50
SECTOR_IDEAL_MAX_FILES = 800

# 最大 Sector 数量（防止过度分解）
MAX_SECTORS = 8

# 跳过的目录（非源代码）
SKIP_DIRS = frozenset({
    ".git", ".svn", "node_modules", "__pycache__", ".tox",
    "build", "out", "dist", "target", "vendor", "third_party",
    ".gradle", ".idea", ".vscode", ".next", ".nuxt",
    "coverage", ".nyc_output", ".pytest_cache", ".mypy_cache",
})


class SectorManager:
    """
    管理项目的 Sector 分解与调度。

    职责：
      1. 评估项目规模，判断是否需要 Sector 化
      2. 用 LLM 将项目分解为 N 个 Sector
      3. 为每个 Sector 创建独立的 BlackboardPartition
      4. 协调多个 Sector 的并行分析
    """

    def __init__(
        self,
        project_root: Path,
        blackboard: Blackboard,
        config: Any,
        telemetry: Optional[asyncio.Queue] = None,
    ):
        self.project_root = project_root
        self.blackboard = blackboard
        self.config = config
        self.telemetry = telemetry
        self.sectors: list[Sector] = []
        self._dir_stats: dict[str, int] = {}  # 顶层目录 → 文件数

    # ─── 公共接口 ─────────────────────────────────────────────────────────────

    def needs_sector_decomposition(self) -> bool:
        """
        快速判断项目是否需要 Sector 化分解。
        纯 I/O 操作，不使用 LLM。
        """
        file_count, total_size, dir_stats = self._scan_project_scale()
        self._dir_stats = dir_stats
        return (
            file_count > LARGE_PROJECT_FILE_THRESHOLD
            or total_size > LARGE_PROJECT_SIZE_THRESHOLD
        )

    async def decompose(self) -> list[Sector]:
        """
        Phase 0: 将项目分解为多个 Sector。

        流程：
          1. 扫描目录结构（纯 I/O）
          2. 读取关键文档（README, ARCHITECTURE 等）
          3. 用 LLM 生成 Sector 划分方案
          4. 验证并修正 Sector 路径
          5. 按攻击面价值排序

        返回排序后的 Sector 列表。
        """
        await self._emit("sector_decomposition_started", {
            "project": str(self.project_root),
        })

        # Step 1: 扫描目录结构
        file_count, total_size, dir_stats = self._scan_project_scale()
        self._dir_stats = dir_stats

        await self._emit("sector_project_scanned", {
            "file_count": file_count,
            "total_size_mb": total_size // 1_000_000,
            "top_dirs": len(dir_stats),
        })

        # Step 2: 读取关键文档
        doc_summary = self._read_key_documents()

        # Step 3: 用 LLM 生成分解方案
        sorted_dirs = sorted(
            dir_stats.items(), key=lambda x: x[1], reverse=True
        )[:25]

        sectors = await self._llm_decompose(
            file_count, total_size, sorted_dirs, doc_summary
        )

        # Step 4: 验证路径
        validated = []
        for s in sectors:
            abs_path = self.project_root / s.path
            if abs_path.is_dir():
                validated.append(s)
            else:
                # 尝试模糊匹配：带前缀再试
                found = False
                for prefix in ("src/", "lib/", "pkg/", "internal/", "cmd/"):
                    alt = self.project_root / prefix / s.path
                    if alt.is_dir():
                        s.path = f"{prefix}{s.path}"
                        validated.append(s)
                        found = True
                        break
                if not found:
                    # 最后尝试：大小写不敏感匹配顶层目录
                    path_lower = s.path.lower().strip("/")
                    for existing_dir in self.project_root.iterdir():
                        if existing_dir.is_dir() and existing_dir.name.lower() == path_lower:
                            s.path = existing_dir.name
                            validated.append(s)
                            break

        # Step 4b: 如果 LLM 路径全部失败，回退到启发式分解
        if not validated and sorted_dirs:
            await self._emit("sector_llm_fallback", {
                "reason": f"LLM produced {len(sectors)} sectors but all paths failed validation",
            })
            validated = self._heuristic_decompose(sorted_dirs)

        # Step 5: 排序
        self.sectors = sorted(validated, key=lambda s: s.priority)

        await self._emit("sector_decomposition_complete", {
            "sectors": [
                {"name": s.name, "path": s.path, "priority": s.priority}
                for s in self.sectors
            ],
        })

        return self.sectors

    def create_partition(self, sector: Sector) -> "BlackboardPartition":
        """为指定 Sector 创建独立的 Blackboard 分区。"""
        return self.blackboard.create_partition(sector.id)

    def get_sector_work_dir(self, sector: Sector) -> Path:
        """返回 Sector 对应的收窄 work_dir。"""
        return self.project_root / sector.path

    # ─── 内部实现 ─────────────────────────────────────────────────────────────

    def _scan_project_scale(self) -> tuple[int, int, dict[str, int]]:
        """
        快速扫描项目规模。纯 I/O，不使用 LLM。

        返回: (文件计数, 总字节数, {顶层目录: 文件数})
        """
        file_count = 0
        total_size = 0
        top_dirs: dict[str, int] = {}

        try:
            for root, dirs, files in os.walk(str(self.project_root)):
                # 跳过非源代码目录
                dirs[:] = [d for d in dirs if d not in SKIP_DIRS]

                file_count += len(files)
                for f in files:
                    try:
                        total_size += os.path.getsize(os.path.join(root, f))
                    except OSError:
                        pass

                # 统计顶层子目录的文件分布
                rel = os.path.relpath(root, str(self.project_root))
                top_dir = rel.split(os.sep)[0] if os.sep in rel else rel
                if top_dir != ".":
                    top_dirs[top_dir] = top_dirs.get(top_dir, 0) + len(files)

                # 快速截断：已确认是超大项目
                if file_count > LARGE_PROJECT_FILE_THRESHOLD * 2:
                    break
        except Exception:
            pass

        return file_count, total_size, top_dirs

    def _read_key_documents(self) -> str:
        """读取项目的关键文档文件，提取摘要。"""
        doc_files = [
            "README.md", "README.rst", "README.txt", "README",
            "ARCHITECTURE.md", "DESIGN.md", "CONTRIBUTING.md",
            "docs/README.md", "docs/architecture.md",
            "doc/README.md", "doc/architecture.md",
        ]

        collected = []
        total_chars = 0
        max_total = 8000  # 限制总文档字符数，防止 prompt 膨胀

        for doc_name in doc_files:
            doc_path = self.project_root / doc_name
            if doc_path.is_file():
                try:
                    content = doc_path.read_text(encoding="utf-8", errors="ignore")
                    # 截取前 2000 字符
                    truncated = content[:2000]
                    if total_chars + len(truncated) > max_total:
                        break
                    collected.append(f"--- {doc_name} ---\n{truncated}")
                    total_chars += len(truncated)
                except Exception:
                    pass

        return "\n\n".join(collected) if collected else "(no documentation found)"

    async def _llm_decompose(
        self,
        file_count: int,
        total_size: int,
        sorted_dirs: list[tuple[str, int]],
        doc_summary: str,
    ) -> list[Sector]:
        """
        用 LLM 将项目分解为 Sector。

        如果 LLM 不可用（SDK 导入失败等），回退到基于目录结构的启发式分解。
        """
        dir_overview = "\n".join(
            f"  {d}: ~{c} files" for d, c in sorted_dirs
        )

        decompose_prompt = (
            f"You are a security architecture analyst performing ATTACK SURFACE DECOMPOSITION.\n\n"
            f"## Project Overview\n"
            f"- Total files: ~{file_count}\n"
            f"- Total size: ~{total_size // 1_000_000}MB\n"
            f"- Location: {self.project_root}\n\n"
            f"## Directory Structure (top-level, by file count)\n{dir_overview}\n\n"
            f"## Project Documentation\n{doc_summary}\n\n"
            f"## Your Task\n"
            f"Decompose this project into {min(MAX_SECTORS, max(3, len(sorted_dirs) // 3))} "
            f"SECURITY-FOCUSED analysis sectors. Each sector should be:\n"
            f"1. A cohesive subsystem with clear security boundaries\n"
            f"2. Small enough for focused analysis (ideally {SECTOR_IDEAL_MIN_FILES}-{SECTOR_IDEAL_MAX_FILES} files)\n"
            f"3. Prioritized by ATTACK SURFACE VALUE (network-facing > auth > data-processing > internal)\n\n"
            f"## Output Format\n"
            f"Return EXACTLY this format for each sector, separated by blank lines:\n\n"
            f"SECTOR: <name>\n"
            f"PATH: <relative directory path from project root>\n"
            f"DESCRIPTION: <one-line description of what this subsystem does>\n"
            f"ATTACK_SURFACE: <what untrusted input reaches this subsystem>\n"
            f"PRIORITY: <0=highest, 1=high, 2=medium, 3=low>\n\n"
            f"IMPORTANT:\n"
            f"- PATH must be an actual directory that exists in the project\n"
            f"- Use directories from the list above\n"
            f"- Do NOT suggest analyzing the whole project as one sector\n"
            f"- Focus on CODE directories, skip docs/tests/examples/configs\n"
        )

        try:
            drone = Drone(
                task_id="S-decomposer",
                task_desc=decompose_prompt,
                drone_role="scope-definer",
                work_dir=self.project_root,
                root_dir=self.project_root.parent,
                config=self.config,
            )

            result = await asyncio.wait_for(drone.execute(), timeout=300.0)
            return self._parse_sector_output(result)

        except Exception as e:
            await self._emit("sector_llm_fallback", {
                "reason": f"{type(e).__name__}: {e}",
            })
            # 回退到启发式分解
            return self._heuristic_decompose(sorted_dirs)

    def _parse_sector_output(self, raw: str) -> list[Sector]:
        """解析 LLM 输出的 Sector 定义。"""
        if not raw:
            return self._heuristic_decompose(
                sorted(self._dir_stats.items(), key=lambda x: x[1], reverse=True)[:25]
            )

        sectors: list[Sector] = []
        # 按 SECTOR: 分割
        blocks = re.split(r'(?:^|\n)SECTOR:\s*', raw, flags=re.IGNORECASE)

        for i, block in enumerate(blocks):
            if not block.strip():
                continue

            name = block.split("\n")[0].strip()

            path_m = re.search(r'PATH:\s*(.+)', block, re.IGNORECASE)
            desc_m = re.search(r'DESCRIPTION:\s*(.+)', block, re.IGNORECASE)
            atk_m = re.search(r'ATTACK_SURFACE:\s*(.+)', block, re.IGNORECASE)
            pri_m = re.search(r'PRIORITY:\s*(\d+)', block, re.IGNORECASE)

            if not path_m:
                continue

            path_str = path_m.group(1).strip().strip("'\"` ")
            # 清理路径：去掉可能的前导 ./
            path_str = path_str.lstrip("./")

            sector_id = f"sector-{re.sub(r'[^a-z0-9]', '-', name.lower())[:30]}"

            sectors.append(Sector(
                id=sector_id,
                name=name,
                path=path_str,
                description=desc_m.group(1).strip() if desc_m else name,
                attack_surface=atk_m.group(1).strip() if atk_m else "unknown",
                priority=int(pri_m.group(1)) if pri_m else i,
                estimated_files=self._dir_stats.get(
                    path_str.split("/")[0] if "/" in path_str else path_str, 0
                ),
            ))

        # 限制最大数量
        return sectors[:MAX_SECTORS]

    def _heuristic_decompose(
        self, sorted_dirs: list[tuple[str, int]]
    ) -> list[Sector]:
        """
        启发式分解：当 LLM 不可用时的回退方案。
        基于目录名称和文件数量自动划分 Sector。
        """
        # 安全价值启发表：目录名 → (优先级, 攻击面描述)
        security_hints: dict[str, tuple[int, str]] = {
            "auth": (0, "Authentication and authorization flows"),
            "login": (0, "User login and session management"),
            "api": (0, "API endpoints handling external requests"),
            "server": (0, "Server-side request processing"),
            "http": (0, "HTTP protocol handling"),
            "net": (0, "Network layer and protocol parsing"),
            "network": (0, "Network communication layer"),
            "web": (1, "Web-facing components"),
            "routes": (1, "URL routing and request handling"),
            "handlers": (1, "Request/event handlers"),
            "controllers": (1, "MVC controllers handling user input"),
            "middleware": (1, "Request/response middleware pipeline"),
            "crypto": (1, "Cryptographic operations"),
            "security": (1, "Security-related logic"),
            "session": (1, "Session management"),
            "upload": (1, "File upload and processing"),
            "parser": (1, "Data parsing and deserialization"),
            "db": (2, "Database interaction layer"),
            "database": (2, "Database queries and ORM"),
            "models": (2, "Data models and validation"),
            "storage": (2, "Data storage and retrieval"),
            "service": (2, "Business logic services"),
            "services": (2, "Business logic services"),
            "core": (2, "Core application logic"),
            "engine": (2, "Processing engine"),
            "lib": (2, "Shared library code"),
            "pkg": (2, "Package implementations"),
            "internal": (2, "Internal implementation"),
            "src": (2, "Main source code"),
            "app": (2, "Application entry point"),
            "cmd": (3, "CLI commands"),
            "utils": (3, "Utility functions"),
            "helpers": (3, "Helper utilities"),
            "config": (3, "Configuration management"),
            "ui": (3, "User interface components"),
            "frontend": (3, "Frontend assets and code"),
            "static": (3, "Static file serving"),
        }

        # 排除的目录
        skip_names = {
            "docs", "doc", "documentation", "test", "tests", "testing",
            "examples", "example", "samples", "sample", "scripts",
            "tools", "contrib", "benchmarks", "fixtures", ".github",
            "assets", "images", "icons", "fonts", "vendor", "third_party",
        }

        sectors: list[Sector] = []
        for dir_name, file_count in sorted_dirs:
            if dir_name.lower() in skip_names:
                continue
            if file_count < SECTOR_IDEAL_MIN_FILES // 2:
                continue

            # 查找最佳匹配的安全提示
            dir_lower = dir_name.lower()
            priority = 3  # 默认低优先级
            attack_surface = "Internal logic and data processing"

            for hint_key, (hint_pri, hint_atk) in security_hints.items():
                if hint_key in dir_lower:
                    priority = hint_pri
                    attack_surface = hint_atk
                    break

            sector_id = f"sector-{re.sub(r'[^a-z0-9]', '-', dir_lower)[:30]}"
            sectors.append(Sector(
                id=sector_id,
                name=dir_name.capitalize(),
                path=dir_name,
                description=f"Source code in {dir_name}/ ({file_count} files)",
                attack_surface=attack_surface,
                priority=priority,
                estimated_files=file_count,
            ))

        # 按优先级排序，取前 MAX_SECTORS 个
        sectors.sort(key=lambda s: (s.priority, -s.estimated_files))
        return sectors[:MAX_SECTORS]

    # ─── 遥测 ─────────────────────────────────────────────────────────────────

    async def _emit(self, event_type: str, data: dict):
        """向遥测队列发送事件。格式与 Cerebrum._emit() 一致: {"event": ..., "data": ...}"""
        if self.telemetry is not None:
            try:
                self.telemetry.put_nowait({
                    "event": event_type,
                    "data": data,
                })
            except asyncio.QueueFull:
                pass
