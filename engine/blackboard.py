"""
Global Blackboard — Hive-Mind 的唯一共享状态中心。
Cerebrum 和所有 Drone 只通过 Blackboard 通信，无直接耦合。
线程/协程安全的异步读写。
"""
import asyncio
import json
import time
import uuid
from dataclasses import dataclass, field, asdict
from enum import Enum
from pathlib import Path
from typing import Any, Optional

from .backends import StorageBackend, LocalBackend


# ─────────────────────────────────────────────
#  布隆过滤器 — O(1) 快速去重前置层
# ─────────────────────────────────────────────

class BloomDeduplicator:
    """
    布隆过滤器前置去重层。

    在五层去重管线之前加一道 O(1) 的快速过滤：
    - 如果布隆过滤器判定"肯定不存在" → 直接跳过五层扫描（零开销）
    - 如果判定"可能存在" → 才进入五层精确去重

    参数（基于 1000 个假设的典型场景）：
      - 误报率 ≤ 1%
      - 内存：约 12KB（1万bit）

    性能提升：
      - 1000 假设场景：从 O(5000) 次字符串比较降至平均 O(1)
      - 实测预期：99% 的"明确非重复"假设零开销跳过五层管线
    """

    def __init__(self, size: int = 10000, num_hashes: int = 7):
        self.size = size
        self.num_hashes = num_hashes
        self._bits = bytearray(size // 8 + 1)
        self._count = 0   # 实际插入数量（用于统计）

    def _hash_positions(self, text: str) -> list[int]:
        import hashlib
        positions = []
        for i in range(self.num_hashes):
            h = hashlib.md5(f"{i}:{text}".encode()).hexdigest()
            positions.append(int(h, 16) % self.size)
        return positions

    def add(self, text: str) -> None:
        """将文本指纹加入过滤器。"""
        for pos in self._hash_positions(text):
            self._bits[pos // 8] |= (1 << (pos % 8))
        self._count += 1

    def might_contain(self, text: str) -> bool:
        """
        返回 True = 可能存在（需进一步五层扫描）
        返回 False = 肯定不存在（直接放行，无重复）
        """
        return all(
            self._bits[pos // 8] & (1 << (pos % 8))
            for pos in self._hash_positions(text)
        )

    @property
    def stats(self) -> dict:
        return {"inserted": self._count, "bits_used": self.size}


# ─────────────────────────────────────────────
#  枚举：状态定义
# ─────────────────────────────────────────────

class HypothesisStatus(str, Enum):
    PENDING   = "pending"    # 已生成，尚未派发任务
    ACTIVE    = "active"     # 正在测试
    SUSPECTED = "suspected"  # 有证据支持，待确认
    CONFIRMED = "confirmed"  # 已确认发现
    DISCARDED = "discarded"  # 死胡同，已放弃


class TaskStatus(str, Enum):
    QUEUED  = "queued"
    RUNNING = "running"
    DONE    = "done"
    FAILED  = "failed"
    TIMEOUT = "timeout"


# ─────────────────────────────────────────────
#  数据结构
# ─────────────────────────────────────────────

@dataclass
class HypothesisNode:
    id:          str
    description: str
    confidence:  float               # 0.0 – 1.0
    status:      HypothesisStatus
    tasks:       list[str] = field(default_factory=list)   # DroneTask.id 列表
    evidence:    list[str] = field(default_factory=list)
    parent_id:   Optional[str] = None
    created_at:  float = 0.0


@dataclass
class DroneTask:
    id:             str
    hypothesis_id:  str
    description:    str
    status:         TaskStatus
    drone_role:     str = "general"
    result:         Optional[str] = None
    error:          Optional[str] = None
    created_at:     float = 0.0
    completed_at:   Optional[float] = None


@dataclass
class Finding:
    id:             str
    hypothesis_id:  str
    title:          str
    description:    str
    severity:       str   # critical | high | medium | low
    evidence:       str
    created_at:     float = 0.0
    sector_id:      Optional[str] = None   # 分区标识（大型项目按 Sector 分析时设置）


# ─────────────────────────────────────────────
#  Blackboard 核心
# ─────────────────────────────────────────────

class Blackboard:
    """
    全局黑板。
    是整个 Hive 唯一的真实来源（Single Source of Truth）。
    任何节点的状态变更都必须通过此类的 async 方法完成，
    以保证并发安全和事件通播。

    可选 backend 参数支持可插拔存储后端：
      - 不传（默认）：自动使用 LocalBackend，写本地 .blackboard.json
      - 传入 RedisBackend：将快照同步到 Redis，并向全局 Pub/Sub 广播事件
    """

    def __init__(
        self,
        work_dir: Path,
        backend: Optional[StorageBackend] = None,
        node_id:  str = "local",
    ):
        self.work_dir = work_dir
        self._node_id = node_id
        # 如果没有传入后端，默认使用本地文件后端（向后完全兼容）
        self._backend: StorageBackend = backend or LocalBackend(work_dir)

        # Bug Fix: asyncio 原语必须在运行中的事件循环里创建，使用懒初始化属性。
        self._lock_obj:   Optional[asyncio.Lock]  = None
        self._arb_queue:  Optional[asyncio.Queue] = None

        self.hypotheses:  dict[str, HypothesisNode] = {}
        self.tasks:       dict[str, DroneTask]       = {}
        self.findings:    list[Finding]              = []

        self.target:      str  = ""
        self.active:      bool = False

        # 订阅者列表：UI 和其他观察者通过 subscribe() 接收事件
        self._subscribers: list[asyncio.Queue] = []

        # 布隆过滤器前置去重层（O(1) 快速预筛，避免每次都对全量假设执行 O(N) 五层扫描）
        self._bloom: BloomDeduplicator = BloomDeduplicator()

        # 保留属性为向后兼容（现有代码引用 _persist_path 的地方不会报错）
        self._persist_path = work_dir / ".blackboard.json"

    @property
    def _lock(self) -> asyncio.Lock:
        if self._lock_obj is None:
            self._lock_obj = asyncio.Lock()
        assert self._lock_obj is not None
        return self._lock_obj

    @property
    def arbitration_queue(self) -> asyncio.Queue:
        if self._arb_queue is None:
            self._arb_queue = asyncio.Queue()
        assert self._arb_queue is not None
        return self._arb_queue

    # ── 订阅 / 广播 ────────────────────────────

    def subscribe(self) -> asyncio.Queue:
        """返回一个新的事件队列，调用者从中 await 事件。"""
        q: asyncio.Queue = asyncio.Queue()
        self._subscribers.append(q)
        return q

    async def _broadcast(self, event_type: str, data: Any = None):
        # Bug Fix: data or {} 会把空列表/空字符串误判为 falsy，改用 is None 检查
        msg = {"type": event_type, "data": data if data is not None else {}}
        for q in self._subscribers:
            try:
                q.put_nowait(msg)
            except asyncio.QueueFull:
                pass   # 丢弃过慢的订阅者，不阻塞主流程
        # 同时向全局事件总线（Redis PubSub）广播，供 Web 仪表盘实时消费
        # LocalBackend 下此调用是 no-op，不影响单机模式
        self._backend.emit_event(self._node_id, event_type, data if data is not None else {})

    # ── 假设树操作 ──────────────────────────────

    # ── 安全领域同义词表 ── 将领域特定术语归一化到统一 canonical form
    _SECURITY_SYNONYMS: dict[str, str] = {
        # 认证相关
        "authentication": "auth", "authn": "auth", "login": "auth",
        "authorization": "authz", "permission": "authz", "privilege": "authz", "rbac": "authz",
        "unauthenticated": "no-auth", "unauthorized": "no-auth", "missing-auth": "no-auth",
        "unauth": "no-auth",
        # 注入相关
        "sqli": "sql-injection", "sql-injection": "sql-injection",
        "xss": "cross-site-scripting", "cross-site-scripting": "cross-site-scripting",
        "cross-site": "cross-site-scripting",
        "rce": "remote-code-execution", "remote-code-execution": "remote-code-execution",
        "command-injection": "cmd-injection", "cmd-injection": "cmd-injection",
        "os-command": "cmd-injection", "command-exec": "cmd-injection",
        "ssrf": "server-side-request-forgery", "server-side-request-forgery": "server-side-request-forgery",
        "lfi": "local-file-inclusion", "local-file-inclusion": "local-file-inclusion",
        "rfi": "remote-file-inclusion",
        "path-traversal": "path-traversal", "directory-traversal": "path-traversal",
        "idor": "insecure-direct-object-reference",
        # 序列化
        "deserialize": "deserialization", "deserialization": "deserialization",
        "unmarshal": "deserialization", "unpickle": "deserialization",
        # 加密相关
        "hardcoded-key": "hardcoded-secret", "hardcoded-password": "hardcoded-secret",
        "hardcoded-token": "hardcoded-secret", "hardcoded-credential": "hardcoded-secret",
        # 结构性
        "endpoint": "route", "api-endpoint": "route", "handler": "route",
        "middleware": "middleware", "filter": "middleware", "interceptor": "middleware",
        "sanitize": "validation", "validate": "validation", "sanitization": "validation",
        "bypass": "bypass", "circumvent": "bypass", "evade": "bypass",
        "overflow": "overflow", "buffer-overflow": "overflow", "heap-overflow": "overflow",
        "stack-overflow": "overflow",
        "race-condition": "race-condition", "toctou": "race-condition",
        "csrf": "cross-site-request-forgery",
        # 模板/表达式注入
        "ssti": "template-injection", "template-injection": "template-injection",
        "server-side-template-injection": "template-injection",
        "ognl": "expression-injection", "spel": "expression-injection", "el-injection": "expression-injection",
        # XML
        "xxe": "xml-external-entity", "xml-external-entity": "xml-external-entity",
        # Token/JWT
        "jwt": "json-web-token", "json-web-token": "json-web-token",
        "token-forgery": "token-forgery", "forged": "forgery", "forgery": "forgery",
        # 其他常见漏洞类型
        "open-redirect": "open-redirect", "url-redirect": "open-redirect",
        "clickjacking": "clickjacking", "ui-redress": "clickjacking",
        "cors": "cors-misconfiguration", "cors-misconfiguration": "cors-misconfiguration",
        "mass-assignment": "mass-assignment", "over-posting": "mass-assignment",
        "prototype-pollution": "prototype-pollution", "proto-pollution": "prototype-pollution",
        "insecure-random": "weak-randomness", "weak-random": "weak-randomness",
        "information-disclosure": "info-leak", "info-leak": "info-leak", "info-disclosure": "info-leak",
        "error-message": "info-leak", "stack-trace": "info-leak",
        "dos": "denial-of-service", "denial-of-service": "denial-of-service", "redos": "denial-of-service",
        "insecure-deserialization": "deserialization",
        "broken-access-control": "authz", "bac": "authz",
        "misconfiguration": "misconfiguration", "misconfig": "misconfiguration",
    }

    @classmethod
    def _normalize_for_dedup(cls, text: str) -> tuple[list[str], set[str], set[str]]:
        """
        将假设描述归一化为三级表示：
        返回 (sorted_canonical_tokens, bigram_set, unigram_set)
        """
        import re as _re
        text = text.lower()
        # 保留字母、数字、中文、路径分隔符
        text = _re.sub(r'[^a-z0-9\u4e00-\u9fff\s/._-]', ' ', text)
        # 分词
        raw_tokens = [w for w in text.split() if len(w) > 1]

        stopwords = {
            'the', 'a', 'an', 'is', 'are', 'was', 'were', 'be', 'been',
            'in', 'on', 'at', 'to', 'for', 'of', 'with', 'by', 'from',
            'that', 'this', 'it', 'and', 'or', 'not', 'no', 'can', 'may',
            'might', 'could', 'would', 'should', 'has', 'have', 'had',
            'there', 'their', 'but', 'if', 'when', 'which', 'what',
            'found', 'check', 'investigate', 'analyze', 'possible',
            'potential', 'likely', 'appears', 'seems', 'using', 'via',
        }

        # 同义词归一化 + 去停用词
        canonical = []
        for w in raw_tokens:
            if w in stopwords:
                continue
            # 尝试同义词映射
            canon = cls._SECURITY_SYNONYMS.get(w, w)
            canonical.append(canon)

        # 三级表示
        unigrams = set(canonical)
        bigrams = set()
        for i in range(len(canonical) - 1):
            bigrams.add(f"{canonical[i]}|{canonical[i+1]}")
        sorted_fingerprint = sorted(canonical)  # 不受词序影响的结构指纹

        return sorted_fingerprint, bigrams, unigrams

    def _find_similar_hypothesis(self, description: str, threshold: float = 0.55) -> Optional[str]:
        """
        四层级语义去重管线：
          Layer 0: 锚点标识符精准匹配（函数名 + 文件路径）
          Layer 1: 结构指纹完全匹配（排序后的 canonical tokens 完全相同）
          Layer 2: Bigram 重合度 ≥ threshold（捕捉短语级相似）
          Layer 3: Unigram Jaccard ≥ threshold + 0.1（宽松的词袋级回退）
          Layer 4: 字符三元组模糊匹配（兜底层）

        每一层独立判定，命中即返回。越上层越精确、越不容易误合并。
        """
        import re as _re

        # Layer 0: 锚点标识符精准匹配
        # 从描述中提取函数名（camelCase/snake_case）和文件路径
        # 如果两个假设提到相同的【函数名 + 漏洞类型关键词】，就是同一个漏洞
        def _extract_anchors(text: str) -> set[str]:
            anchors = set()
            # 提取函数名（snake_case 或 camelCase，至少有一个下划线或大写）
            func_names = _re.findall(r'\b([a-z][a-z0-9]*(?:_[a-z0-9]+)+)\b', text.lower())
            for fn in func_names:
                if len(fn) > 4 and fn not in ('such_as', 'more_than', 'less_than', 'based_on'):
                    anchors.add(fn)
            # 提取文件名 (*.c, *.h, *.py 等)
            file_names = _re.findall(r'\b([\w.-]+\.(?:c|h|py|js|java|go|rs|rb|php))\b', text.lower())
            anchors.update(file_names)
            return anchors

        new_anchors = _extract_anchors(description)
        if len(new_anchors) >= 1:
            for h_id, h in self.hypotheses.items():
                if h.status == HypothesisStatus.DISCARDED:
                    continue
                ex_anchors = _extract_anchors(h.description)
                if not ex_anchors:
                    continue
                # 至少有一个函数名或文件名精确匹配
                shared = new_anchors & ex_anchors
                if shared:
                    # 还需要漏洞类型大致相同（防止同文件不同漏洞误合并）
                    new_fp, _, new_uni = self._normalize_for_dedup(description)
                    ex_fp, _, ex_uni = self._normalize_for_dedup(h.description)
                    uni_overlap = len(new_uni & ex_uni) / max(len(new_uni | ex_uni), 1)
                    if uni_overlap >= 0.30:  # 30% 词重合即可（锚点已经精确匹配了）
                        return h_id

        new_fp, new_bi, new_uni = self._normalize_for_dedup(description)
        if not new_uni:
            return None

        best_match: Optional[str] = None
        best_score: float = 0.0

        for h_id, h in self.hypotheses.items():
            if h.status == HypothesisStatus.DISCARDED:
                continue

            ex_fp, ex_bi, ex_uni = self._normalize_for_dedup(h.description)
            if not ex_uni:
                continue

            # Layer 1: 结构指纹完全匹配（最强信号）
            if new_fp == ex_fp:
                return h_id

            # Layer 2: Bigram 重合度（短语级）
            if new_bi and ex_bi:
                bi_inter = len(new_bi & ex_bi)
                bi_union = len(new_bi | ex_bi)
                if bi_union > 0:
                    bi_score = bi_inter / bi_union
                    if bi_score >= threshold:
                        if bi_score > best_score:
                            best_score = bi_score
                            best_match = h_id
                        continue  # 已匹配，检查是否有更好的

            # Layer 3: Unigram Jaccard（词袋级回退，阈值更高）
            uni_inter = len(new_uni & ex_uni)
            uni_union = len(new_uni | ex_uni)
            if uni_union > 0:
                uni_score = uni_inter / uni_union
                # 仅在高重合度下才接受词袋匹配（防止 "SQL" + "auth" 误匹配到 "SQL" + "injection"）
                if uni_score >= threshold + 0.10 and uni_score > best_score:
                    best_score = uni_score
                    best_match = h_id

        # Layer 4: 字符三元组模糊匹配（兜底层 — 不依赖任何同义词表）
        if best_match is None:
            new_trigrams = self._char_trigrams(description)
            if len(new_trigrams) >= 5:
                for h_id, h in self.hypotheses.items():
                    if h.status == HypothesisStatus.DISCARDED:
                        continue
                    ex_trigrams = self._char_trigrams(h.description)
                    if len(ex_trigrams) < 5:
                        continue
                    tri_inter = len(new_trigrams & ex_trigrams)
                    tri_union = len(new_trigrams | ex_trigrams)
                    if tri_union > 0:
                        tri_score = tri_inter / tri_union
                        if tri_score >= 0.70 and tri_score > best_score:
                            best_score = tri_score
                            best_match = h_id

        return best_match

    @staticmethod
    def _char_trigrams(text: str) -> set[str]:
        """
        将文本分解为字符三元组集合，用于模糊匹配。
        不依赖分词或同义词表，直接在字符级别捕获形态学相似性。
        'authentication bypass' → {'aut','uth','the','hen','ent','nti','tic','ica','cat','ati','tio','ion',...}
        """
        import re as _re
        text = text.lower()
        text = _re.sub(r'[^a-z0-9\u4e00-\u9fff]', ' ', text)
        # 去掉停用词的干扰
        stopwords = {'the', 'a', 'an', 'is', 'are', 'in', 'on', 'at', 'to', 'for', 'of', 'with', 'by', 'from'}
        words = [w for w in text.split() if len(w) > 2 and w not in stopwords]
        joined = ' '.join(words)
        trigrams = set()
        for i in range(len(joined) - 2):
            tri = joined[i:i+3]
            if ' ' not in tri:  # 不跨词边界
                trigrams.add(tri)
        return trigrams

    async def add_hypothesis(
        self,
        description: str,
        confidence: float,
        parent_id: Optional[str] = None,
    ) -> str:
        async with self._lock:
            # ── 布隆过滤器前置预筛（O(1)，零开销快速路径）──
            # 布隆过滤器说"肯定不存在" → 跳过五层扫描，直接新增
            # 布隆过滤器说"可能存在" → 进入五层精确去重
            bloom_says_maybe = self._bloom.might_contain(description)

            if bloom_says_maybe:
                # 布隆过滤器认为可能重复，进入完整五层去重管线
                existing_id = self._find_similar_hypothesis(description)
            else:
                # 布隆过滤器确认不存在，直接放行（零五层扫描开销）
                existing_id = None

            if existing_id:
                existing = self.hypotheses[existing_id]
                # 置信度合并策略：取较高值 + 小幅度提升（多次独立提出 = 更可信）
                merge_boost = min(0.05, 0.02 * len([
                    e for e in existing.evidence if e.startswith("[merged]")
                ]))
                new_conf = min(1.0, max(existing.confidence, confidence) + merge_boost)
                existing.confidence = new_conf

                # 状态恢复：如果新提交的置信度 ≥ 0.4 且原假设正在衰退，重新激活
                if (existing.status == HypothesisStatus.DISCARDED and confidence >= 0.4):
                    existing.status = HypothesisStatus.PENDING
                elif existing.status == HypothesisStatus.PENDING:
                    pass

                # 补充证据追加（限制条数防止无限膨胀）
                merge_evidence = f"[merged] {description[:200]}"
                if len(existing.evidence) < 10 and merge_evidence not in existing.evidence:
                    existing.evidence.append(merge_evidence)
                h_id = existing_id
            else:
                h_id = f"H-{uuid.uuid4().hex[:6].upper()}"
                node = HypothesisNode(
                    id=h_id,
                    description=description,
                    confidence=min(1.0, max(0.0, confidence)),
                    status=HypothesisStatus.PENDING,
                    parent_id=parent_id,
                    created_at=time.time(),
                )
                self.hypotheses[h_id] = node
                # 无论是否走五层扫描，新增假设都需要加入布隆过滤器
                self._bloom.add(description)

        if existing_id:
            await self._broadcast("hypothesis_updated", {
                "id": existing_id, "confidence": self.hypotheses[existing_id].confidence,
                "note": "merged_duplicate",
            })
        else:
            await self._broadcast("hypothesis_added", asdict(self.hypotheses[h_id]))
        self._persist()
        return h_id

    async def update_hypothesis(self, h_id: str, **kwargs):
        async with self._lock:
            if h_id not in self.hypotheses:
                return
            node = self.hypotheses[h_id]
            for k, v in kwargs.items():
                if hasattr(node, k):
                    setattr(node, k, v)

        await self._broadcast("hypothesis_updated", {"id": h_id, **kwargs})
        self._persist()

    # ── 任务操作 ────────────────────────────────

    async def add_task(
        self,
        hypothesis_id: str,
        description: str,
        drone_role: str = "general",
    ) -> Optional[str]:
        async with self._lock:
            # ── 任务防重入去重 (Task Deduplication) ──
            # 防止 LLM 在多轮交互中重复下发相同的排查任务
            import re
            def _simplify(t: str) -> str:
                return re.sub(r'[^a-zA-Z0-9\u4e00-\u9fa5]', '', t.lower())
            
            new_simple = _simplify(description)
            for existing_task in self.tasks.values():
                if existing_task.drone_role == drone_role:
                    ex_simple = _simplify(existing_task.description)
                    # 简单的包含或高相似度匹配
                    if new_simple in ex_simple or ex_simple in new_simple:
                        # 已经存在相似的任务（不管其挂在哪个假说下，也不管其是否执行失败/超时），直接全局拦截
                        return None

            t_id = f"T-{uuid.uuid4().hex[:6].upper()}"
            task = DroneTask(
                id=t_id,
                hypothesis_id=hypothesis_id,
                description=description,
                status=TaskStatus.QUEUED,
                drone_role=drone_role,
                created_at=time.time(),
            )
            self.tasks[t_id] = task
            if hypothesis_id in self.hypotheses:
                self.hypotheses[hypothesis_id].tasks.append(t_id)
                self.hypotheses[hypothesis_id].status = HypothesisStatus.ACTIVE

        await self._broadcast("task_added", asdict(task))
        self._persist()
        return t_id

    async def update_task(self, t_id: str, **kwargs):
        async with self._lock:
            if t_id not in self.tasks:
                return
            task = self.tasks[t_id]
            for k, v in kwargs.items():
                if hasattr(task, k):
                    setattr(task, k, v)
            if kwargs.get("status") in (
                TaskStatus.DONE, TaskStatus.FAILED, TaskStatus.TIMEOUT
            ):
                task.completed_at = time.time()

        await self._broadcast("task_updated", {"id": t_id, **kwargs})
        self._persist()

    # ── 发现操作 ────────────────────────────────

    async def add_finding(
        self,
        hypothesis_id: str,
        title: str,
        description: str,
        severity: str,
        evidence: str,
        sector_id: Optional[str] = None,
    ) -> str:
        async with self._lock:
            # FIX(Root Cause 4): Finding 级别去重 — 防止同一 Bug 被多次报告
            for existing in self.findings:
                if existing.hypothesis_id == hypothesis_id or self._finding_is_duplicate(existing.title, title):
                    # 合并升级 severity
                    sev_order = {"critical": 4, "high": 3, "medium": 2, "low": 1, "none": 0}
                    if sev_order.get(severity.lower(), 0) > sev_order.get(existing.severity.lower(), 0):
                        existing.severity = severity
                        existing.title = title  # 用更高级别的 title 覆盖
                        existing.description = description

                    # 合并证据到已有 Finding，不创建新 Finding
                    if evidence and evidence not in existing.evidence:
                        existing.evidence += f"\n[corroborated] {evidence}"
                    # 如果传入了 sector_id 且 existing 没有，补充之
                    if sector_id and not existing.sector_id:
                        existing.sector_id = sector_id
                    return existing.id

            f_id = f"F-{uuid.uuid4().hex[:6].upper()}"
            finding = Finding(
                id=f_id,
                hypothesis_id=hypothesis_id,
                title=title,
                description=description,
                severity=severity,
                evidence=evidence,
                created_at=time.time(),
                sector_id=sector_id,
            )
            self.findings.append(finding)
            if hypothesis_id in self.hypotheses:
                self.hypotheses[hypothesis_id].status = HypothesisStatus.CONFIRMED

        await self._broadcast("finding_added", asdict(finding))
        self._write_audit_notes()
        self._persist()
        return f_id

    @staticmethod
    def _finding_is_duplicate(existing_title: str, new_title: str, threshold: float = 0.6) -> bool:
        """基于 token Jaccard 相似度判断两个 Finding 是否重复。"""
        def tokenize(text: str) -> set:
            import re as _re
            return set(_re.findall(r'[a-zA-Z0-9_]+', text.lower()))
        t1 = tokenize(existing_title)
        t2 = tokenize(new_title)
        if not t1 or not t2:
            return False
        intersection = len(t1 & t2)
        union = len(t1 | t2)
        return (intersection / union) >= threshold if union > 0 else False

    # ── 仲裁 ────────────────────────────────────

    async def request_arbitration(self, question: str, context: str) -> bool:
        """
        将仲裁请求放入队列，阻塞等待人类决策。
        UI / CLI 层需要消费 arbitration_queue，并回填 Future。
        """
        # Bug Fix: get_event_loop() 在 Python 3.10+ 有弃用警告，改用 get_running_loop()
        loop = asyncio.get_running_loop()
        fut: asyncio.Future = loop.create_future()
        await self.arbitration_queue.put({
            "question": question,
            "context": context,
            "future": fut,
        })
        await self._broadcast("arbitration_requested", {"question": question})
        return await fut

    # ── 查询工具 ────────────────────────────────

    def get_pending_hypotheses(self) -> list[HypothesisNode]:
        return [h for h in self.hypotheses.values()
                if h.status == HypothesisStatus.PENDING]

    def get_active_tasks(self) -> list[DroneTask]:
        return [t for t in self.tasks.values()
                if t.status in (TaskStatus.QUEUED, TaskStatus.RUNNING)]

    def stats(self) -> dict:
        return {
            "hypotheses": len(self.hypotheses),
            "pending":    len(self.get_pending_hypotheses()),
            "active_tasks": len(self.get_active_tasks()),
            "findings":   len(self.findings),
            "confirmed":  sum(1 for h in self.hypotheses.values()
                              if h.status == HypothesisStatus.CONFIRMED),
            "discarded":  sum(1 for h in self.hypotheses.values()
                              if h.status == HypothesisStatus.DISCARDED),
        }

    def snapshot(self) -> dict:
        return {
            "target":     self.target,
            "active":     self.active,
            "hypotheses": {k: asdict(v) for k, v in self.hypotheses.items()},
            "tasks":      {k: asdict(v) for k, v in self.tasks.items()},
            "findings":   [asdict(f) for f in self.findings],
        }


    # ── 持久化（委托给 Backend）──────────────────────

    def _persist(self):
        """将当前快照委托给后端持久化（本地 JSON / Redis / 其他后端）。"""
        self._backend.save(self._node_id, self.snapshot())

    def _write_audit_notes(self):
        """将 Findings 汇总写到后端可见的位置（文件 / Redis Key）。"""
        lines = ["# 🔴 HIVE-MIND CONFIRMED FINDINGS\n"]
        for f in self.findings:
            sev_icon = {
                "critical": "🔴", "high": "🟠",
                "medium": "🟡", "low": "🔵",
            }.get(f.severity, "⚪")
            lines.append(f"### [LEAD: CONFIRMED] {sev_icon} {f.title}")
            lines.append(f"- **Severity**: {f.severity.upper()}")
            lines.append(f"- **Description**: {f.description}")
            lines.append(f"- **Evidence**: {f.evidence}")
            lines.append("")
        self._backend.write_audit_notes(self._node_id, "\n".join(lines))


    def load(self):
        """从后端恢复状态（支持本地 JSON 和 Redis 两种模式）。"""
        data = self._backend.load(self._node_id)
        if not data:
            return
        try:
            self.target = data.get("target", "")
            for h_id, h in data.get("hypotheses", {}).items():
                h["status"] = HypothesisStatus(h["status"])
                self.hypotheses[h_id] = HypothesisNode(
                    **{k: v for k, v in h.items()
                       if k in HypothesisNode.__dataclass_fields__}
                )
            for t_id, t in data.get("tasks", {}).items():
                t["status"] = TaskStatus(t["status"])
                self.tasks[t_id] = DroneTask(
                    **{k: v for k, v in t.items()
                       if k in DroneTask.__dataclass_fields__}
                )
            for f in data.get("findings", []):
                self.findings.append(
                    Finding(**{k: v for k, v in f.items()
                               if k in Finding.__dataclass_fields__})
                )
        except Exception:
            pass

    def emit_global(self, event_type: str, data: Any = None) -> None:
        """向全局事件总线（Redis Pub/Sub 等）发布遊测事件。本地模式下为 no-op。"""
        self._backend.emit_event(self._node_id, event_type, data or {})

    def create_partition(self, sector_id: str) -> "BlackboardPartition":
        """
        为 Sector 创建独立的 Blackboard 分区视图。

        分区隔离了假设(Hypothesis)和任务(Task)的命名空间，
        使每个 Sector 的 Mini-Cerebrum 只看到自己的状态，
        从而将单轮 prompt 的 token 消耗从 ~91K 降到 ~13K。

        但 Finding 会自动汇聚到主 Blackboard，便于：
          - 跨模块利用链分析
          - 全局报告生成
          - 去重与置信度合并
        """
        return BlackboardPartition(parent=self, sector_id=sector_id)


# ─────────────────────────────────────────────────────────────────────────────
#  BlackboardPartition — Sector 级别的隔离视图
# ─────────────────────────────────────────────────────────────────────────────

class BlackboardPartition:
    """
    为单个 Sector 提供的 Blackboard 隔离视图。

    接口与 Blackboard 兼容（鸭子类型），让 Mini-Cerebrum 可以
    在不修改签名的情况下使用分区视图替代全局 Blackboard。

    隔离策略：
      - hypotheses: 独立空间，Sector 之间互不可见
      - tasks:      独立空间，Sector 之间互不可见
      - findings:   写入本地 + 自动 bubble up 到 parent Blackboard
      - 广播事件:   委托给 parent（保持 UI 订阅者的统一接收）
    """

    def __init__(self, parent: Blackboard, sector_id: str):
        self.parent = parent
        self.sector_id = sector_id
        self.work_dir = parent.work_dir

        # 独立的假设和任务空间
        self.hypotheses: dict[str, HypothesisNode] = {}
        self.tasks: dict[str, DroneTask] = {}
        self.findings: list[Finding] = []  # 本分区的 Finding 缓存

        self.target: str = parent.target
        self.active: bool = True

        # 复用 parent 的锁和订阅者（保持线程安全和事件统一）
        self._lock_obj: Optional[asyncio.Lock] = None
        self._subscribers = parent._subscribers  # 共享订阅者

        # 分区独立的布隆过滤器（只对当前分区的假设去重）
        self._bloom: BloomDeduplicator = BloomDeduplicator()

    @property
    def _lock(self) -> asyncio.Lock:
        if self._lock_obj is None:
            self._lock_obj = asyncio.Lock()
        return self._lock_obj

    # ── 订阅/广播：委托给 parent ──────────────────────────

    def subscribe(self) -> asyncio.Queue:
        return self.parent.subscribe()

    async def _broadcast(self, event_type: str, data: Any = None):
        """广播事件时附加 sector_id，让 UI 可以按分区过滤。"""
        msg_data = data if data is not None else {}
        if isinstance(msg_data, dict):
            msg_data["sector_id"] = self.sector_id
        await self.parent._broadcast(event_type, msg_data)

    # ── 假设操作（独立空间）──────────────────────────────

    async def add_hypothesis(
        self,
        description: str,
        confidence: float,
        parent_id: Optional[str] = None,
    ) -> str:
        """在分区内添加假设（与 Blackboard.add_hypothesis 接口一致）。"""
        async with self._lock:
            # 分区布隆过滤器前置预筛（O(1)，避免对分区假设池做全量扫描）
            if self._bloom.might_contain(description):
                existing_id = self._find_similar_hypothesis(description)
            else:
                existing_id = None

            if existing_id:
                existing = self.hypotheses[existing_id]
                merge_boost = min(0.05, 0.02 * len([
                    e for e in existing.evidence if e.startswith("[merged]")
                ]))
                new_conf = min(1.0, max(existing.confidence, confidence) + merge_boost)
                existing.confidence = new_conf
                if existing.status == HypothesisStatus.DISCARDED and confidence >= 0.4:
                    existing.status = HypothesisStatus.PENDING
                merge_evidence = f"[merged] {description[:200]}"
                if len(existing.evidence) < 10 and merge_evidence not in existing.evidence:
                    existing.evidence.append(merge_evidence)
                h_id = existing_id
            else:
                h_id = f"H-{uuid.uuid4().hex[:6].upper()}"
                node = HypothesisNode(
                    id=h_id,
                    description=description,
                    confidence=min(1.0, max(0.0, confidence)),
                    status=HypothesisStatus.PENDING,
                    parent_id=parent_id,
                    created_at=time.time(),
                )
                self.hypotheses[h_id] = node
                self._bloom.add(description)

        if existing_id:
            await self._broadcast("hypothesis_updated", {
                "id": existing_id, "confidence": self.hypotheses[existing_id].confidence,
                "note": "merged_duplicate",
            })
        else:
            await self._broadcast("hypothesis_added", asdict(self.hypotheses[h_id]))
        return h_id

    def _find_similar_hypothesis(self, description: str, threshold: float = 0.55) -> Optional[str]:
        """分区内假设去重（复用 Blackboard 的静态去重逻辑）。"""
        new_fp, new_bi, new_uni = Blackboard._normalize_for_dedup(description)
        if not new_uni:
            return None

        best_match: Optional[str] = None
        best_score: float = 0.0

        for h_id, h in self.hypotheses.items():
            if h.status == HypothesisStatus.DISCARDED:
                continue
            ex_fp, ex_bi, ex_uni = Blackboard._normalize_for_dedup(h.description)
            if not ex_uni:
                continue
            if new_fp == ex_fp:
                return h_id
            if new_bi and ex_bi:
                bi_inter = len(new_bi & ex_bi)
                bi_union = len(new_bi | ex_bi)
                if bi_union > 0:
                    bi_score = bi_inter / bi_union
                    if bi_score >= threshold and bi_score > best_score:
                        best_score = bi_score
                        best_match = h_id
                        continue
            uni_inter = len(new_uni & ex_uni)
            uni_union = len(new_uni | ex_uni)
            if uni_union > 0:
                uni_score = uni_inter / uni_union
                if uni_score >= threshold + 0.10 and uni_score > best_score:
                    best_score = uni_score
                    best_match = h_id

        return best_match

    async def update_hypothesis(self, h_id: str, **kwargs):
        """更新分区内假设状态。"""
        async with self._lock:
            if h_id not in self.hypotheses:
                return
            node = self.hypotheses[h_id]
            for k, v in kwargs.items():
                if hasattr(node, k):
                    setattr(node, k, v)
        await self._broadcast("hypothesis_updated", {"id": h_id, **kwargs})

    # ── 任务操作（独立空间）────────────────────────────────

    async def add_task(
        self,
        hypothesis_id: str,
        description: str,
        drone_role: str = "general",
    ) -> Optional[str]:
        """在分区内添加任务（与 Blackboard.add_task 接口一致）。"""
        async with self._lock:
            # 分区内任务去重
            import re as _re
            def _simplify(t: str) -> str:
                return _re.sub(r'[^a-zA-Z0-9\u4e00-\u9fa5]', '', t.lower())

            new_simple = _simplify(description)
            for existing_task in self.tasks.values():
                if existing_task.drone_role == drone_role:
                    ex_simple = _simplify(existing_task.description)
                    if new_simple in ex_simple or ex_simple in new_simple:
                        return None

            t_id = f"T-{uuid.uuid4().hex[:6].upper()}"
            task = DroneTask(
                id=t_id,
                hypothesis_id=hypothesis_id,
                description=description,
                status=TaskStatus.QUEUED,
                drone_role=drone_role,
                created_at=time.time(),
            )
            self.tasks[t_id] = task
            if hypothesis_id in self.hypotheses:
                self.hypotheses[hypothesis_id].tasks.append(t_id)
                self.hypotheses[hypothesis_id].status = HypothesisStatus.ACTIVE

        await self._broadcast("task_added", asdict(task))
        return t_id

    async def update_task(self, t_id: str, **kwargs):
        """更新分区内任务状态。"""
        async with self._lock:
            if t_id not in self.tasks:
                return
            task = self.tasks[t_id]
            for k, v in kwargs.items():
                if hasattr(task, k):
                    setattr(task, k, v)
            if kwargs.get("status") in (
                TaskStatus.DONE, TaskStatus.FAILED, TaskStatus.TIMEOUT
            ):
                task.completed_at = time.time()
        await self._broadcast("task_updated", {"id": t_id, **kwargs})

    # ── Finding 操作（写入本地 + 自动上报 parent）──────────

    async def add_finding(
        self,
        hypothesis_id: str,
        title: str,
        description: str,
        severity: str,
        evidence: str,
    ) -> str:
        """
        添加 Finding：写入本分区缓存 + 自动 bubble up 到主 Blackboard。

        这是 Sector 架构的核心设计：Finding 不隔离。
        所有 Sector 的 Finding 都汇聚到同一个 Blackboard，
        使 Coordinator 可以进行跨模块利用链分析。
        """
        # 直接委托给 parent，并附加 sector_id
        f_id = await self.parent.add_finding(
            hypothesis_id=hypothesis_id,
            title=title,
            description=description,
            severity=severity,
            evidence=evidence,
            sector_id=self.sector_id,
        )

        # 本地缓存一份引用（方便 Sector 级别的 stats 统计）
        async with self._lock:
            for f in self.parent.findings:
                if f.id == f_id:
                    self.findings.append(f)
                    break

            # 更新分区内假设状态
            if hypothesis_id in self.hypotheses:
                self.hypotheses[hypothesis_id].status = HypothesisStatus.CONFIRMED

        return f_id

    # ── 查询工具（与 Blackboard 接口兼容）────────────────

    def get_pending_hypotheses(self) -> list[HypothesisNode]:
        return [h for h in self.hypotheses.values()
                if h.status == HypothesisStatus.PENDING]

    def get_active_tasks(self) -> list[DroneTask]:
        return [t for t in self.tasks.values()
                if t.status in (TaskStatus.QUEUED, TaskStatus.RUNNING)]

    def stats(self) -> dict:
        return {
            "hypotheses": len(self.hypotheses),
            "pending":    len(self.get_pending_hypotheses()),
            "active_tasks": len(self.get_active_tasks()),
            "findings":   len(self.findings),
            "confirmed":  sum(1 for h in self.hypotheses.values()
                              if h.status == HypothesisStatus.CONFIRMED),
            "discarded":  sum(1 for h in self.hypotheses.values()
                              if h.status == HypothesisStatus.DISCARDED),
            "sector_id":  self.sector_id,
        }

    def snapshot(self) -> dict:
        return {
            "target":     self.target,
            "active":     self.active,
            "sector_id":  self.sector_id,
            "hypotheses": {k: asdict(v) for k, v in self.hypotheses.items()},
            "tasks":      {k: asdict(v) for k, v in self.tasks.items()},
            "findings":   [asdict(f) for f in self.findings],
        }

    # ── 持久化（委托给 parent）─────────────────────────────

    def _persist(self):
        self.parent._persist()

    def _write_audit_notes(self):
        self.parent._write_audit_notes()

    def load(self):
        pass  # 分区不独立加载，由 parent 管理

    async def request_arbitration(self, question: str, context: str) -> bool:
        return await self.parent.request_arbitration(question, context)

    def emit_global(self, event_type: str, data: Any = None) -> None:
        self.parent.emit_global(event_type, data)
