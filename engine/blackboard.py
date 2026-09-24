"""
Global Blackboard — huntzero 的唯一共享状态中心。
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
#  枚举：状态定义
# ─────────────────────────────────────────────

class HypothesisStatus(str, Enum):
    PENDING   = "pending"    # 已生成，尚未派发任务
    ACTIVE    = "active"     # 正在测试
    SUSPECTED = "suspected"  # 有证据支持，待确认
    CONFIRMED = "confirmed"  # 已确认发现
    DISCARDED = "discarded"  # 死胡同，已放弃


def classify_hypothesis_polarity(description: str) -> str:
    """
    判断一个假设声明的极性。

    - "positive": 声明存在漏洞/弱点/风险（这是安全扫描系统应该追踪的假设）
    - "negative": 声明不存在漏洞、代码是安全的、没有可利用的弱点

    基于语法模式检测而非简单关键词列表，识别以下否定结构：
      [Subject] (has|have|is|are|contains|shows|exhibits) (no|not|never|none) [security_noun]
      [Subject] (is|are) (secure|safe|not vulnerable|not exploitable)
      (no|not|never) [security_noun] (in|within|found|detected|identified|present|exists)
      [Subject] (does not|doesn't|cannot|can't) [exploit_verb]
    """
    import re as _re

    lower = description.lower().strip()
    if not lower:
        return "positive"

    # 安全相关名词和动词集合
    security_nouns = (
        r"vulnerability|vulnerabilities|exploit|exploits|flaw|flaws|"
        r"bug|bugs|weakness|weaknesses|defect|defects|risk|risks|"
        r"issue|issues|hole|holes|overflow|uaf|use.after.free|"
        r"leak|leaks|bypass|bypasses|race.condition|toctou|"
        r"injection|injections|sqli|xss|csrf|ssrf|"
        r"memory.safety.problem|memory.safety.issue|buffer.error"
    )
    exploit_verbs = (
        r"contain|contains|have|has|exhibit|exhibits|show|shows|"
        r"present|presents|demonstrate|demonstrates|suffer|suffers"
    )

    # 模式1: Subject + (is/are/has/have/contains) + (no/not/never/none) + security_noun
    p1 = _re.compile(
        rf"\b\w+(?:\s+\w+){{0,6}}\s+"
        rf"(?:is|are|has|have|contains|shows|exhibits)\s+"
        rf"(?:no|not|never|none)\s+(?:\w+\s+){{0,3}}(?:{security_nouns})",
        _re.IGNORECASE,
    )

    # 模式2: Subject + (is/are) + (secure/safe/not vulnerable/not exploitable)
    p2 = _re.compile(
        rf"\b\w+(?:\s+\w+){{0,6}}\s+"
        rf"(?:is|are)\s+(?:secure|safe|not\s+vulnerable|not\s+exploitable|not\s+at\s+risk)",
        _re.IGNORECASE,
    )

    # 模式3: (no/not/never) + security_noun + (in/within/found/detected/identified/present/exists)
    p3 = _re.compile(
        rf"\b(?:no|not|never)\s+(?:\w+\s+){{0,3}}(?:{security_nouns})"
        rf"\s+(?:in|within|found|detected|identified|present|exists|observed)",
        _re.IGNORECASE,
    )

    # 模式4: Subject + (does not / doesn't / cannot / can't) + exploit_verb
    p4 = _re.compile(
        rf"\b\w+(?:\s+\w+){{0,6}}\s+"
        rf"(?:does\s+not|doesn't|cannot|can't)\s+(?:{exploit_verbs})",
        _re.IGNORECASE,
    )

    if p1.search(lower) or p2.search(lower) or p3.search(lower) or p4.search(lower):
        return "negative"

    return "positive"


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
    polarity:    str = "positive"     # "positive" = asserts existence of vuln
                                      # "negative" = asserts absence of vuln / safety


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
    # PoC 验证字段
    poc_status:     str = "pending"        # pending | generated | verified_success | verified_failed | error
    poc_output:     Optional[str] = None   # PoC 执行输出
    poc_verified_at: Optional[float] = None  # 验证时间戳
    # 利用前置条件（Exploit Prerequisites）
    prerequisites:  Optional["ExploitPrerequisites"] = None  # 利用所需的前置条件
    # ── OSV / 依赖漏洞字段（可选）──────────────────────────────────────────
    finding_type:   str = "zero_day"       # "zero_day" | "dependency_vuln"
    cve_id:         str = ""               # e.g. CVE-2024-1234
    package_name:   str = ""               # e.g. lodash
    package_version: str = ""              # e.g. 4.17.20
    fixed_version:  str = ""               # e.g. 4.17.21


@dataclass
class ExploitPrerequisites:
    """
    利用前置条件元组。

    在报告利用漏洞时，明确标注利用该漏洞所需的前置条件，
    帮助区分"理论可利用"和"实际可利用"的漏洞。
    """
    auth_required: bool = False               # 是否需要认证
    network_access: str = "external"         # 网络位置：external | internal | localhost
    privilege_level: str = "none"            # 所需权限：none | user | admin | system
    env_configs: list[str] = field(default_factory=list)   # 所需配置项（如 debug=true）
    cve_dependencies: list[str] = field(default_factory=list)  # 依赖的其他 CVE
    tool_requirements: list[str] = field(default_factory=list)  # 所需工具（如 ysoserial）
    is_satisfiable: bool = True               # 当前目标是否满足这些条件
    unmet_conditions: list[str] = field(default_factory=list)  # 不满足的条件列表


# ─────────────────────────────────────────────────────────────────────────────

# ────────────────────────────────────────────────────────────
# ─────────────────────────────────────────────────────────────────────────────
#  贝叶斯置信度传播引擎（可学习版本）
# ─────────────────────────────────────────────────────────────────────────────

class BayesianConfidenceEngine:
    """
    沿 Hypothesis 父子链传播置信度（贝叶斯推理）+ 从历史数据学习因果强度。
    
    使用方法：
        engine = BayesianConfidenceEngine(blackboard.hypotheses, strength_path=work_dir)
        updated_count = engine.update_confidences()
        engine.record_outcome("H-ABC123", confirmed=True)  # PoC 验证触发学习
        engine.save()
    """

    DEFAULT_STRENGTH = 0.85
    LAPLACE_ALPHA = 1.0
    MIN_SAMPLES_TO_USE_LEARNED = 3

    def __init__(
        self,
        hypotheses: dict,
        strength_path: "Path" = None,
        strength_matrix: "dict" = None,
    ):
        self.hypotheses = hypotheses
        self.strength_matrix: dict = strength_matrix or {}
        self._strength_path = strength_path
        self._outcomes: list = []
        if strength_path:
            loaded = self._load_from_file(strength_path)
            if loaded:
                self.strength_matrix.update(loaded)

    def _get_strength(self, parent_id: str, child_id: str) -> float:
        key = f"{parent_id}->{child_id}"
        if key not in self.strength_matrix:
            return self.DEFAULT_STRENGTH
        entry = self.strength_matrix[key]
        if isinstance(entry, dict) and entry.get("n", 0) >= self.MIN_SAMPLES_TO_USE_LEARNED:
            return entry["strength"]
        return self.DEFAULT_STRENGTH

    def record_outcome(self, h_id: str, confirmed: bool) -> None:
        h = self.hypotheses.get(h_id)
        if not h:
            return
        record = {"h_id": h_id, "confirmed": confirmed, "parent_id": h.parent_id}
        record["_idx"] = len(self._outcomes)
        self._outcomes.append(record)
        if h.parent_id:
            self._update_strength_for_pair(h.parent_id, h_id, confirmed)

    def _update_strength_for_pair(self, parent_id: str, child_id: str, child_confirmed: bool) -> None:
        key = f"{parent_id}->{child_id}"
        parent_records = [r for r in self._outcomes if r["h_id"] == parent_id]
        child_records  = [r for r in self._outcomes if r["h_id"] == child_id]
        if not parent_records or not child_records:
            return
        parent_time = max((r.get("_idx", 0) for r in parent_records), default=0)
        child_after = [r for r in child_records if r.get("_idx", 0) >= parent_time]
        n_confirmed = sum(1 for r in child_after if r["confirmed"])
        n_total     = len(child_after)
        n_child_total     = len(child_records)
        n_child_confirmed = sum(1 for r in child_records if r["confirmed"])
        base_rate = (n_child_confirmed + self.LAPLACE_ALPHA) / (n_child_total + 2 * self.LAPLACE_ALPHA)
        if n_total > 0:
            conditional = (n_confirmed + self.LAPLACE_ALPHA) / (n_total + 2 * self.LAPLACE_ALPHA)
            strength = min(1.0, max(0.0, conditional / base_rate)) if base_rate > 0 else self.DEFAULT_STRENGTH
        else:
            strength = min(1.0, max(0.0, base_rate))
        existing = self.strength_matrix.get(key, {"strength": self.DEFAULT_STRENGTH, "n": 0})
        new_n = existing.get("n", 0) + 1
        self.strength_matrix[key] = {
            "strength": round(strength, 4),
            "n": new_n,
            "base_rate": round(base_rate, 4),
        }

    def learn_from_outcomes(self, outcomes: list) -> dict:
        self._outcomes = []
        for idx, rec in enumerate(outcomes):
            rec["_idx"] = idx
            self._outcomes.append(rec)
            if rec.get("parent_id"):
                self._update_strength_for_pair(rec["parent_id"], rec["h_id"], rec["confirmed"])
        return self.strength_matrix

    def save(self) -> bool:
        if not self._strength_path:
            return False
        try:
            path = Path(self._strength_path)
            path.mkdir(parents=True, exist_ok=True)
            fpath = path / ".bayesian_strength.json"
            data = {
                "strength_matrix": self.strength_matrix,
                "meta": {"total_outcomes": len(self._outcomes), "default_strength": self.DEFAULT_STRENGTH}
            }
            fpath.write_text(json.dumps(data, indent=2, ensure_ascii=False), encoding="utf-8")
            return True
        except Exception:
            return False

    def _load_from_file(self, path) -> dict:
        try:
            fpath = Path(path) / ".bayesian_strength.json"
            if fpath.exists():
                return json.loads(fpath.read_text(encoding="utf-8")).get("strength_matrix", {})
        except Exception:
            pass
        return {}

    def _propagate_single(self, h_id: str, visited: set) -> float:
        if h_id in visited:
            return 0.0
        visited.add(h_id)
        h = self.hypotheses.get(h_id)
        if not h:
            return 0.0
        if not h.parent_id:
            return h.confidence
        parent_conf = self._propagate_single(h.parent_id, visited.copy())
        strength = self._get_strength(h.parent_id, h_id)
        propagated = parent_conf * strength
        return 0.6 * propagated + 0.4 * h.confidence

    def propagate_all(self) -> dict:
        return {
            h_id: min(1.0, max(0.0, self._propagate_single(h_id, set())))
            for h_id in self.hypotheses
        }

    def update_confidences(self) -> int:
        posteriors = self.propagate_all()
        updated = 0
        for h_id, posterior in posteriors.items():
            if h_id in self.hypotheses:
                old = self.hypotheses[h_id].confidence
                self.hypotheses[h_id].confidence = posterior
                if abs(old - posterior) > 0.01:
                    updated += 1
        return updated

    def get_strength_stats(self) -> dict:
        learned = sum(
            1 for v in self.strength_matrix.values()
            if isinstance(v, dict) and v.get("n", 0) >= self.MIN_SAMPLES_TO_USE_LEARNED
        )
        return {
            "total_pairs": len(self.strength_matrix),
            "learned_pairs": learned,
            "outcomes_in_memory": len(self._outcomes),
            "default_strength": self.DEFAULT_STRENGTH,
        }

def _find_similar_in(hypotheses: dict, description: str, threshold: float = 0.55) -> Optional[str]:
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
        for h_id, h in hypotheses.items():
            ex_anchors = _extract_anchors(h.description)
            if not ex_anchors:
                continue
            # 至少有一个函数名或文件名精确匹配
            shared = new_anchors & ex_anchors
            if shared:
                # 还需要漏洞类型大致相同（防止同文件不同漏洞误合并）
                new_fp, _, new_uni = Blackboard._normalize_for_dedup(description)
                ex_fp, _, ex_uni = Blackboard._normalize_for_dedup(h.description)
                uni_overlap = len(new_uni & ex_uni) / max(len(new_uni | ex_uni), 1)
                if uni_overlap >= 0.30:  # 30% 词重合即可（锚点已经精确匹配了）
                    return h_id

    new_fp, new_bi, new_uni = Blackboard._normalize_for_dedup(description)
    if not new_uni:
        return None

    best_match: Optional[str] = None
    best_score: float = 0.0

    for h_id, h in hypotheses.items():
        ex_fp, ex_bi, ex_uni = Blackboard._normalize_for_dedup(h.description)
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
        new_trigrams = Blackboard._char_trigrams(description)
        if len(new_trigrams) >= 5:
            for h_id, h in hypotheses.items():
                ex_trigrams = Blackboard._char_trigrams(h.description)
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
      - 传入自定义 StorageBackend 实现：接管快照持久化与事件发布
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
        return _find_similar_in(self.hypotheses, description, threshold)

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
    ) -> Optional[str]:
        # FIX: 数据层防御 — 拒绝否定性假设，确保 blackboard 的领域不变性
        if classify_hypothesis_polarity(description) == "negative":
            return None

        async with self._lock:
            existing_id = self._find_similar_hypothesis(description)

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
                    polarity=classify_hypothesis_polarity(description),
                )
                self.hypotheses[h_id] = node

            # 在锁内 snapshot 需要广播的数据，避免锁外读取被并发修改
            broadcast_data = None
            if existing_id:
                broadcast_data = {
                    "id": existing_id,
                    "confidence": self.hypotheses[existing_id].confidence,
                    "note": "merged_duplicate",
                }
                await self._broadcast("hypothesis_updated", broadcast_data)
            else:
                broadcast_data = asdict(self.hypotheses[h_id])
                await self._broadcast("hypothesis_added", broadcast_data)

            # FIX: 在锁内调用 _persist()，防止竞态条件
            # 原代码在锁外调用 _persist()，导致其他协程可能在持久化前修改数据
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

            # FIX: 在锁内调用 _persist()，防止竞态条件
            self._persist()

        await self._broadcast("hypothesis_updated", {"id": h_id, **kwargs})

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
            # 注意：仅在同一 hypothesis_id 下做去重，避免跨假设误杀
            import re
            def _simplify(t: str) -> str:
                return re.sub(r'[^a-zA-Z0-9\u4e00-\u9fa5]', '', t.lower())
            
            new_simple = _simplify(description)
            for existing_task in self.tasks.values():
                # 仅在同一假设下检查，防止跨假设误杀
                if existing_task.hypothesis_id != hypothesis_id:
                    continue
                if existing_task.drone_role == drone_role:
                    ex_simple = _simplify(existing_task.description)
                    # 包含匹配（注意：只有长度差异大才用，小幅差异不算）
                    if new_simple in ex_simple and len(ex_simple) - len(new_simple) <= 10:
                        return None
                    if ex_simple in new_simple and len(new_simple) - len(ex_simple) <= 10:
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

            # FIX: 在锁内调用 _persist()，防止竞态条件
            self._persist()

        await self._broadcast("task_added", asdict(task))
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

            # FIX: 在锁内调用 _persist()，防止竞态条件
            self._persist()

        await self._broadcast("task_updated", {"id": t_id, **kwargs})

    # ── 发现操作 ────────────────────────────────

    async def request_finding_reverification(
        self,
        finding_id: str,
        reason: str,
    ) -> Optional[str]:
        """
        基于现有 Finding 重新拉起验证任务。

        当外部验证（PoC 失败、人工复核质疑、Devil's Advocate 反驳）表明某个已确认的
        Finding 可能不成立时，不直接删除 Finding，而是生成一个新的验证任务让 Drone
        重新检查原始证据。

        Args:
            finding_id: 需要重新验证的 Finding ID
            reason: 触发重验证的原因（如 "poc_failed", "devils_advocate_refuted",
                    "manual_review_rejected"）
        Returns:
            新验证任务的 task_id，如果找不到 finding 或原 hypothesis 已被删除则返回 None
        """
        finding = None
        for f in self.findings:
            if f.id == finding_id:
                finding = f
                break
        if not finding:
            return None

        hyp_id = finding.hypothesis_id
        if hyp_id not in self.hypotheses:
            return None

        # 使用带时间戳和 reason 的描述，避免被 task deduplication 拦截
        reverify_desc = (
            f"[RE-VERIFICATION | {reason} | {time.time():.0f}] "
            f"Re-check the validity of this previously confirmed finding.\n\n"
            f"Original Finding: {finding.title}\n"
            f"Original Severity: {finding.severity}\n"
            f"Original Evidence:\n```\n{finding.evidence[:1500]}\n```\n\n"
            f"Your task: Re-examine the exact code paths referenced in the evidence above. "
            f"Determine whether the vulnerability is STILL present and exploitable, "
            f"or whether it has been fixed, mitigated, or was a false positive. "
            f"Report your conclusion with concrete code evidence."
        )

        # 为 hypothesis 附加重验证标记
        existing = self.hypotheses[hyp_id]
        tag = f"[re-verification-requested] {finding_id}: {reason}"
        if len(existing.evidence) < 15 and tag not in existing.evidence:
            existing.evidence.append(tag)

        t_id = await self.add_task(
            hypothesis_id=hyp_id,
            description=reverify_desc,
            drone_role="re-verifier",
        )
        return t_id

    async def add_finding(
        self,
        hypothesis_id: str,
        title: str,
        description: str,
        severity: str,
        evidence: str,
        sector_id: Optional[str] = None,
        **kwargs,
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
                **{k: v for k, v in kwargs.items() if k in Finding.__dataclass_fields__},
            )
            self.findings.append(finding)
            if hypothesis_id in self.hypotheses:
                self.hypotheses[hypothesis_id].status = HypothesisStatus.CONFIRMED

            # FIX: 在锁内调用 _persist()，防止竞态条件
            self._persist()

        await self._broadcast("finding_added", asdict(finding))
        self._write_audit_notes()
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
        lines = ["# 🔴 HUNTZERO CONFIRMED FINDINGS\n"]
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

    def export_exploit_chain_graph(self, output_path: Path) -> bool:
        """
        导出利用链图为 DOT 格式（可用 graphviz 渲染为 PNG/SVG）。
        节点：Hypothesis（按 severity 着色）
        边：parent_id 父子关系 + evidence 关联
        """
        try:
            lines = ["digraph ExploitChains {"]
            lines.append('  rankdir=LR;')
            lines.append('  node [shape=box, style=filled, fontname="Arial"];')
            lines.append('  edge [fontname="Arial", fontsize=10];')
            lines.append("")

            # 颜色映射
            color_map = {
                "critical": "red",
                "high": "orange",
                "medium": "yellow",
                "low": "lightblue",
                "none": "gray",
            }

            # 节点
            for h_id, h in self.hypotheses.items():
                if h.status.value not in ("active", "suspected", "confirmed"):
                    continue
                color = color_map.get(h.severity if hasattr(h, 'severity') else "none", "gray")
                label = h.description[:50].replace('"', "'").replace("\n", " ")
                lines.append(f'  "{h_id}" [label="{label}", fillcolor="{color}", fontcolor=black];')

            lines.append("")

            # 边（父子关系）
            for h_id, h in self.hypotheses.items():
                if h.parent_id and h.parent_id in self.hypotheses:
                    lines.append(f'  "{h.parent_id}" -> "{h_id}";')

            lines.append("")
            lines.append('  // Findings as double-circle nodes')
            for f in self.findings:
                f_node = f"F-{f.id}"
                lines.append(f'  "{f_node}" [label="FINDING: {f.title[:30]}", shape=doublecircle, fillcolor=red];')
                if f.hypothesis_id in self.hypotheses:
                    lines.append(f'  "{f.hypothesis_id}" -> "{f_node}" [style=bold, color=red];')

            lines.append("}")
            output_path.parent.mkdir(parents=True, exist_ok=True)
            output_path.write_text("\n".join(lines), encoding="utf-8")
            return True
        except Exception as e:
            print(f"[export_exploit_chain_graph] Error: {e}")
            return False

    def export_graphml(self, output_path: Path) -> bool:
        """
        导出利用链图为 GraphML 格式（可用 yEd、Cytoscape 打开）。
        """
        try:
            lines = ['<?xml version="1.0" encoding="UTF-8"?>']
            lines.append('<graphml xmlns="http://graphml.graphdrawing.org/xmlns"')
            lines.append('           xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"')
            lines.append('           xsi:schemaLocation="http://graphml.graphdrawing.org/xmlns')
            lines.append('           http://graphml.graphdrawing.org/xmlns/1.0/graphml.xsd">')
            lines.append('  <key id="label" for="node" attr.name="label" attr.type="string"/>')
            lines.append('  <key id="color" for="node" attr.name="color" attr.type="string"/>')
            lines.append('  <key id="severity" for="node" attr.name="severity" attr.type="string"/>')
            lines.append('  <key id="edge_label" for="edge" attr.name="label" attr.type="string"/>')
            lines.append('  <graph id="ExploitChains" edgedefault="directed">')

            # 节点
            for h_id, h in self.hypotheses.items():
                if h.status.value not in ("active", "suspected", "confirmed"):
                    continue
                label = h.description[:50].replace('&', '&amp;').replace('"', '&quot;')
                lines.append(f'    <node id="{h_id}">')
                lines.append(f'      <data key="label">{label}</data>')
                lines.append(f'      <data key="severity">{h.status.value}</data>')
                lines.append(f'    </node>')

            # 边
            edge_id = 0
            for h_id, h in self.hypotheses.items():
                if h.parent_id and h.parent_id in self.hypotheses:
                    lines.append(f'    <edge id="e{edge_id}" source="{h.parent_id}" target="{h_id}">')
                    lines.append(f'      <data key="edge_label">parent-child</data>')
                    lines.append(f'    </edge>')
                    edge_id += 1

            lines.append('  </graph>')
            lines.append('</graphml>')

            output_path.parent.mkdir(parents=True, exist_ok=True)
            output_path.write_text("\n".join(lines), encoding="utf-8")
            return True
        except Exception as e:
            print(f"[export_graphml] Error: {e}")
            return False


    def load(self):
        """
        从后端恢复状态（支持本地 JSON 和 Redis 两种模式）。
        使用原子加载策略：先解析到临时变量，成功后再替换实例状态，
        避免半加载状态（JSON 损坏时不会污染现有对象）。
        """
        data = self._backend.load(self._node_id)
        if not data:
            return

        # 原子加载：先解析到临时容器，验证成功后再替换
        tmp_hypotheses: dict[str, HypothesisNode] = {}
        tmp_tasks: dict[str, DroneTask] = {}
        tmp_findings: list[Finding] = []

        try:
            for h_id, h in data.get("hypotheses", {}).items():
                h_copy = dict(h)
                h_copy["status"] = HypothesisStatus(h_copy["status"])
                tmp_hypotheses[h_id] = HypothesisNode(
                    **{k: v for k, v in h_copy.items()
                       if k in HypothesisNode.__dataclass_fields__}
                )
            for t_id, t in data.get("tasks", {}).items():
                t_copy = dict(t)
                t_copy["status"] = TaskStatus(t_copy["status"])
                tmp_tasks[t_id] = DroneTask(
                    **{k: v for k, v in t_copy.items()
                       if k in DroneTask.__dataclass_fields__}
                )
            for f in data.get("findings", []):
                tmp_findings.append(
                    Finding(**{k: v for k, v in f.items()
                               if k in Finding.__dataclass_fields__})
                )

            # 所有解析成功后才替换，避免半加载状态
            self.target = data.get("target", "")
            self.hypotheses = tmp_hypotheses
            self.tasks = tmp_tasks
            self.findings = tmp_findings
        except Exception as e:
            import sys
            print(f"[Blackboard.load] 状态恢复失败，数据可能已损坏: {e}", file=sys.stderr)

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
    ) -> Optional[str]:
        """在分区内添加假设（与 Blackboard.add_hypothesis 接口一致）。"""
        if classify_hypothesis_polarity(description) == "negative":
            return None

        async with self._lock:
            existing_id = self._find_similar_hypothesis(description)

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
                    polarity=classify_hypothesis_polarity(description),
                )
                self.hypotheses[h_id] = node

        if existing_id:
            await self._broadcast("hypothesis_updated", {
                "id": existing_id, "confidence": self.hypotheses[existing_id].confidence,
                "note": "merged_duplicate",
            })
        else:
            await self._broadcast("hypothesis_added", asdict(self.hypotheses[h_id]))
        return h_id

    def _find_similar_hypothesis(self, description: str, threshold: float = 0.55) -> Optional[str]:
        return _find_similar_in(self.hypotheses, description, threshold)

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

    async def request_finding_reverification(
        self,
        finding_id: str,
        reason: str,
    ) -> Optional[str]:
        """
        基于现有 Finding 重新拉起验证任务。

        当外部验证（PoC 失败、人工复核质疑、Devil's Advocate 反驳）表明某个已确认的
        Finding 可能不成立时，不直接删除 Finding，而是生成一个新的验证任务让 Drone
        重新检查原始证据。

        Args:
            finding_id: 需要重新验证的 Finding ID
            reason: 触发重验证的原因（如 "poc_failed", "devils_advocate_refuted",
                    "manual_review_rejected"）
        Returns:
            新验证任务的 task_id，如果找不到 finding 或原 hypothesis 已被删除则返回 None
        """
        finding = None
        for f in self.findings:
            if f.id == finding_id:
                finding = f
                break
        if not finding:
            return None

        hyp_id = finding.hypothesis_id
        if hyp_id not in self.hypotheses:
            return None

        # 使用带时间戳和 reason 的描述，避免被 task deduplication 拦截
        reverify_desc = (
            f"[RE-VERIFICATION | {reason} | {time.time():.0f}] "
            f"Re-check the validity of this previously confirmed finding.\n\n"
            f"Original Finding: {finding.title}\n"
            f"Original Severity: {finding.severity}\n"
            f"Original Evidence:\n```\n{finding.evidence[:1500]}\n```\n\n"
            f"Your task: Re-examine the exact code paths referenced in the evidence above. "
            f"Determine whether the vulnerability is STILL present and exploitable, "
            f"or whether it has been fixed, mitigated, or was a false positive. "
            f"Report your conclusion with concrete code evidence."
        )

        # 为 hypothesis 附加重验证标记
        existing = self.hypotheses[hyp_id]
        tag = f"[re-verification-requested] {finding_id}: {reason}"
        if len(existing.evidence) < 15 and tag not in existing.evidence:
            existing.evidence.append(tag)

        t_id = await self.add_task(
            hypothesis_id=hyp_id,
            description=reverify_desc,
            drone_role="re-verifier",
        )
        return t_id

    async def add_finding(
        self,
        hypothesis_id: str,
        title: str,
        description: str,
        severity: str,
        evidence: str,
        **kwargs,
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
            **kwargs,
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
