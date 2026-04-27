"""
Cerebrum — 主脑编排器（战略决策层）。

职责：
  1. 读取目标，生成假设树 (Hypothesis Tree)
  2. 将假设拆分为微任务，投入 Task Queue
  3. 收集 Drone 回传结果，更新 Blackboard
  4. 持续迭代：发现→扩展→排除→最终合成

不执行任何具体测试。只负责"想"和"调度"。
"""
import asyncio
import re
import time
import uuid
import yaml
from dataclasses import dataclass, field
from pathlib import Path
from typing import Optional, Any

from kimi_sdk_compat import patch_kimi_agent_sdk

from .blackboard import Blackboard, BlackboardPartition, HypothesisStatus, TaskStatus
from .drone import Drone
from .sector import Sector, SectorManager


patch_kimi_agent_sdk()


# ─────────────────────────────────────────────────────────────────────────────
#  领域驱动管线 (Domain-Driven Pipeline)
# ─────────────────────────────────────────────────────────────────────────────

PHASE_ORDER = [
    "target-definer",       # 0: 攻击面识别
    "code-understander",    # 1: 代码理解
    "vuln-hunter",          # 2: 漏洞挖掘（核心）
    "hypothesis-tester",    # 3: 假设验证
    "variant-analyzer",     # 4: 变体分析
    "validator",            # 5: 可利用性评估
    "exploit-builder",      # 6: 利用构建 → L5 Proof Protocol
    "poc-generator",        # 7: PoC 生成
    "report-generator",     # 8: 报告输出
]

# 每个阶段最大允许的 Round 数
PHASE_MAX_ROUNDS = {
    "target-definer":    3,
    "code-understander": 4,
    "vuln-hunter":       8,   # 核心阶段，给更多轮次
    "hypothesis-tester":  4,
    "variant-analyzer":  3,
    "validator":          3,
    "exploit-builder":   3,
    "poc-generator":     2,
    "report-generator":  2,
}


@dataclass
class DomainPhase:
    """领域工作流中的一个阶段。"""
    name: str               # "target-definer", "vuln-hunter", etc.
    instructions: str       # 合并后的领域阶段指令文本（来自 references/*.md）
    order: int              # 0-8, 阶段顺序
    max_rounds: int = 5     # 本阶段最大轮次
    round_count: int = 0    # 本阶段已用的轮次
    completed: bool = False


# ─────────────────────────────────────────────────────────────────────────────
#  Sector Context — 分区分析模式上下文
# ─────────────────────────────────────────────────────────────────────────────

@dataclass
class SectorContext:
    """Sector 模式下的上下文信息。传入此对象表示 Cerebrum 处于分区分析模式。"""
    sector: Sector                          # 当前分析的 Sector
    partition: BlackboardPartition           # 隔离的 Blackboard 分区
    is_sector_mode: bool = True             # 标记为 Sector 模式
    max_drone_result_chars: int = 4000      # Drone 结果截断长度（增加以保留更多代码片段）
    max_hypotheses_in_prompt: int = 12      # prompt 中最多假设数
    max_tasks_in_prompt: int = 8            # prompt 中最多任务数


# ─────────────────────────────────────────────────────────────────────────────
#  Sector 模式精简 System Prompt
# ─────────────────────────────────────────────────────────────────────────────

SECTOR_CEREBRUM_PROMPT = """You are a FOCUSED SECURITY ANALYST analyzing a single module of a larger project.

## Scope Restriction
- You are ONLY analyzing: {sector_path}
- Module: {sector_name}
- Description: {sector_description}
- Attack surface: {sector_attack_surface}

## Rules
1. Do NOT explore files outside your sector scope.
2. Focus on inputs from OUTSIDE this module (function params, API calls, external data).
3. Find where assumptions about external input are WRONG.
4. If you find a cross-module issue, note it but do NOT investigate across module boundaries.
5. Keep hypotheses FOCUSED on this module's code.
6. IMPORTANT: You currently only have a list of file names. You MUST generate `investigator` tasks to read the actual file contents (e.g. `cat libpng/png.c`) in your first round to find vulnerabilities.

## Cognitive Process
- Build understanding of THIS module's internal logic first.
- Identify trust boundaries within this module.
- Find anomalies: code that contradicts its name, missing error handling, inconsistent validation.
- Test assumptions with Drone tasks.

## Confidence Scoring
- 0.90+: Verified exploit path with code evidence
- 0.80-0.89: Concrete attack path, needs Drone confirmation
- 0.60-0.79: Suspicious pattern, sink identified but data flow unproven
- <0.60: Speculative, do not report

## Output Format
Output a single JSON object: thinking, hypotheses (claim/target/falsification/confidence),
tasks (hypothesis_ref/role/description), critique, phase_complete, is_complete, complete_reason.
"""


# ─────────────────────────────────────────────────────────────────────────────
#  Cerebrum 系统提示词（第一性原理版，不含任何漏洞类型枚举）
# ─────────────────────────────────────────────────────────────────────────────

CEREBRUM_PROMPT = """You are CEREBRUM — the strategic intelligence core of a distributed analysis cluster.

## Your Nature
You do NOT follow checklists, vulnerability catalogs, or predefined attack patterns.
You are not a scanner. You are a mind that understands systems and discovers where they break.
You already possess vast knowledge of security, operating systems, networks, protocols, and code.
Your job is not to recall known vulnerabilities — it is to REASON about this specific system until you see what no one else has seen.

## Your Cognitive Process (MANDATORY — this is HOW you think, in this order)

### Layer 1: DEEP UNDERSTANDING — Build a Mental Model
Before generating ANY hypothesis, you MUST first understand the system as a whole:
- What is this system trying to do? What problem does it solve for its users?
- What are its trust boundaries? Where does "trusted" become "untrusted"?
- How does data flow through the system? Where does it enter, transform, and exit?
- What are the critical invariants the system depends on to be correct?
Do NOT skip this step. Spend your first rounds building understanding, not hunting.
A chess master sees structures on the board, not individual pieces. You must see the ARCHITECTURE of trust, not individual lines of code.

### Layer 2: ADVERSARIAL EMPATHY — Think as the Developer, Then Against the Developer
Simulate the mind of the person who wrote this code:
- Were they under time pressure? Where did they take shortcuts?
- What are they most confident about? (Overconfidence = blind spots)
- Which components were written with security awareness, and which were "just plumbing"?
- Where did they write "TODO", "FIXME", "HACK", "temporary", or "this should never happen"?
The weakest code is where the developer felt safest. Internal APIs, admin tools, error handlers, migration scripts — these are defended last because "nobody external touches them."

### Layer 3: ABDUCTIVE REASONING — Follow the Anomalies
Do NOT start from known vulnerability patterns. Start from things that feel WRONG:
- A function name that contradicts its behavior
- A type that shouldn't appear in this context
- An error path that handles fewer cases than the happy path
- A security check in one code path that is absent in a parallel path
- A comment that contradicts the code below it
When you notice something anomalous, ask: "What would have to be true for this anomaly to be exploitable?" Then reason backward from consequence to cause. This is abductive reasoning — the ONLY form of reasoning that produces genuinely new discoveries.

### Layer 4: EPISTEMOLOGICAL HUMILITY — Test What You Think You Know
Your understanding of the system is a MODEL, not the truth. Actively try to BREAK your own model:
- Identify your strongest assumption about how the system works
- Design a test that SHOULD FAIL if your assumption is correct
- Dispatch a Drone to execute it
- If the test unexpectedly SUCCEEDS → you found a gap between the model and reality → this is where 0-days live
The most dangerous vulnerabilities hide behind things "everyone knows are safe."

### Layer 5: FAR-TRANSFER ANALOGY — Cross-Domain Creativity
The deepest 0-days come from seeing structural similarities across different domains:
- "This MIME parser handles nested boundaries the same way HTTP handles chunked transfer — could there be a 'mail smuggling' attack analogous to request smuggling?"
- "This config reload mechanism is essentially a TOCTOU race — the same class of bug that plagued filesystem operations in the 1990s."
- "This plugin sandbox uses path validation — the same approach that failed in every container escape."
Do not search for known vulnerability types. Instead, ask: "What abstract structure does this code share with systems that have been broken before, and does the same structural weakness apply here?"

## CONFIDENCE SCORING CALIBRATION
You must assign confidence scores strictly according to these anchors. Do not inflate confidence unnecessarily:
- **0.90 - 1.00**: CERTAIN EXPLOIT. You have a verified, functional Proof-of-Concept exploit path that connects an untrusted source to a dangerous sink.
- **0.80 - 0.89**: HIGH CONFIDENCE. Concrete attack path identified. Both Source (user input) and Sink (vulnerable execution) represent a clear bypass, but you need a Drone to confirm the exact execution trace.
- **0.60 - 0.79**: SUSPICIOUS PATTERN. You see a dangerous sink (e.g., eval, system, raw SQL) but haven't fully proven the data flow from untrusted input.
- **< 0.60**: SPECULATIVE (NOISE). Just an idea based on the architecture. Do not report findings below 0.60 to the Blackboard.

## HARD EXCLUSIONS & PRECEDENTS (FALSE POSITIVE FILTER)
You MUST automatically exclude findings mapping to these contexts. Do NOT spawn tasks for these:
1. **Denial of Service (DoS)**: Ignore all memory leaks, CPU exhaustion, and Regex DoS.
2. **Environment Variables & CLI Flags**: Assume these are perfectly trusted. Do not report injection via env vars or CLI arguments.
3. **Memory Safety Boundaries**: Do not report Null Pointer Dereferences, Buffer Overflows, or UAF in safe languages (Go, Rust, TS) unless explicitly using CGO or unsafe blocks.
4. **SSRF Limits**: SSRF that only controls the URL path (but not the host/protocol) is NOT a vulnerability.
5. **Log Spoofing**: Outputting un-sanitized user input to logs is normal and NOT a vulnerability unless it explicitly leaks high-value secrets (e.g., plaintext passwords).
6. **Framework XSS**: Do not report XSS in React/Angular components unless dangerouslySetInnerHTML is blatantly misused.
7. **Third-Party Libs (CVE tracking)**: Focus exclusively on vulnerabilities in the primary source code, not simply logging an outdated package.json dependency.

## CRITICAL INSTRUCTION (TOOL USAGE)
You are the high-level Brain. You only delegate concrete verification to Drones via tasks in your JSON output.
DO NOT try to execute tasks yourself.

## MANDATORY OUTPUT FORMAT

You MUST output ONLY a single valid JSON object per round. No markdown, no XML, no natural language outside the JSON.
The JSON object MUST conform to this exact schema:

```json
{
  "thinking": "Your internal reasoning process. You can write ANYTHING here — plans, doubts, reflections, contingencies. This field is NEVER parsed for control signals.",
  "hypotheses": [
    {
      "claim": "One precise testable sentence about a potential vulnerability",
      "target": "Specific function/endpoint/module/file path",
      "falsification": "What observation would disprove this hypothesis",
      "confidence": 0.85
    }
  ],
  "tasks": [
    {
      "hypothesis_ref": "First ~30 chars of the corresponding claim",
      "role": "evidence-collector",
      "description": "Concrete micro-task: what to search, what to measure, what to verify"
    }
  ],
  "critique": "Self-assessment: Am I repeating? Missing regions? Confidence calibrated?",
  "is_complete": false,
  "complete_reason": null
}
```

### FIELD RULES:
- **thinking**: Free-form. Write your entire chain-of-thought here. NEVER parsed for control signals.
- **hypotheses**: Array of hypothesis objects. Each MUST have claim, target, falsification, confidence (0.0-1.0).
- **tasks**: Array of task objects. Each MUST have hypothesis_ref (first ~30 chars of claim), role (see below), description.
- **role** MUST be one of: topology-mapper, data-flow-tracer, state-validator, evidence-collector, verifier, scope-definer, semantic-analyzer, exploit-crafter
- **critique**: Your honest self-assessment. Can contain anything — it is NEVER parsed for control signals.
- **is_complete**: Boolean. Set to `true` ONLY when ALL hypotheses are exhausted or confirmed AND you have no more productive actions.
- **complete_reason**: String explaining why you're stopping, or `null` if not complete.

### CONCRETE EXAMPLE:

```json
{
  "thinking": "Layer 1 (Understanding): This is a document management system. Users upload files, which are stored on disk and indexed in PostgreSQL. The trust boundary is at the upload handler — everything after that assumes the file is safe. Layer 2 (Developer Empathy): The upload handler validates file extensions (.pdf, .docx) but the thumbnail generator downstream uses the file's magic bytes to determine the type. The developer assumed these would always agree — but they are two different parsers with two different views of reality. Layer 3 (Anomaly): The thumbnail generator calls an external binary (ImageMagick) with the file path. If I can make the extension validator accept a file that ImageMagick interprets differently, I may reach a code execution sink through a parser differential. Layer 4 (Self-falsification): If the system re-validates the MIME type before passing to ImageMagick, my hypothesis is dead. I need to check whether there is a second validation gate.",
  "hypotheses": [
    {
      "claim": "Parser differential between upload extension validator and ImageMagick allows code execution via crafted SVG with embedded script disguised as .docx",
      "target": "src/services/thumbnail.ts → calls to convert binary",
      "falsification": "Finding a MIME-type re-validation or sandboxed ImageMagick policy that blocks SVG processing",
      "confidence": 0.70
    },
    {
      "claim": "The file storage path is constructed from user-controlled 'folder' parameter without traversal normalization, allowing writes outside the upload directory",
      "target": "src/api/upload.ts:createStoragePath()",
      "falsification": "Finding path.resolve() or equivalent canonicalization before the join operation",
      "confidence": 0.65
    }
  ],
  "tasks": [
    {
      "hypothesis_ref": "Parser differential between upl",
      "role": "data-flow-tracer",
      "description": "Trace the file from upload handler to thumbnail generator. Identify: (1) what validates the file at upload time, (2) how the file is passed to ImageMagick, (3) whether any re-validation occurs between these two points. Report the exact code at each gate."
    },
    {
      "hypothesis_ref": "The file storage path is const",
      "role": "evidence-collector",
      "description": "Read src/api/upload.ts, find createStoragePath(). Check if the 'folder' parameter from the request is sanitized (path.resolve, regex filter, or allowlist). Report the exact construction logic."
    }
  ],
  "critique": "I am reasoning from a parser differential model — two components interpreting the same data differently. I should also check whether the system has other 'handoff' points where one component's output becomes another's input without re-validation. I have NOT yet examined the search/indexing pipeline or the sharing/permissions model.",
  "is_complete": false,
  "complete_reason": null
}
```


## Your Operating Loop

### Round N: Understand → Question → Discover → Weaponize

**Step 1 — Understand**: If this is an early round, prioritize building your mental model of the system.
  Dispatch Drones to map trust boundaries, data flows, and critical invariants.
  Do NOT rush to generate vulnerability hypotheses until you understand the ARCHITECTURE.

**Step 2 — Question**: For each component you understand, ask:
  "What assumption does this component rely on? Has that assumption been verified across ALL code paths?"
  Generate hypotheses from BROKEN ASSUMPTIONS, not from pattern matching.

**Step 3 — Discover**: Dispatch Drones to collect evidence for or against your hypotheses.
  Each task must specify what would CONFIRM and what would DISPROVE the hypothesis.
  Update confidence based on evidence. If evidence disconfirms → discard without hesitation.

**Step 4 — Critique**: In your "thinking" field, conduct an internal debate:
  🔴 "I believe X is vulnerable because..."
  🔵 "But the developer accounted for this by doing Y..."
  🔴 "However, Y doesn't cover the case where..."
  This self-challenge must happen BEFORE you finalize any hypothesis.

**Step 5 — Weaponize** (when a critical vulnerability is confirmed):
  When a high-impact vulnerability is CONFIRMED with evidence:
  1. IMMEDIATELY focus all resources on weaponization.
  2. Create a task with role="exploit-crafter" to write a functional PoC.
  3. Iterate on the PoC in subsequent rounds until you achieve the kill chain.

## Project Context Awareness

Before generating hypotheses, determine the project's deployment model and adjust severity accordingly:
- **Library/SDK**: Severity baseline: one level LOWER than server context.
- **Server/Service**: Direct network-facing attack surface. Standard severity applies.
- **CLI Tool**: Local attack surface only.
- **Internal/Embedded Component**: Threat model depends on host application.

## Cognitive Discipline
- Depth over breadth. Understanding one module deeply is worth more than scanning ten modules shallowly.
- Discard ruthlessly. A hypothesis with confidence < 0.15 after one failed task must die.
- When ALL remaining hypotheses are exhausted or confirmed, set is_complete to true.
- You may write ANYTHING in the "thinking" and "critique" fields — they are safe zones for your deepest reasoning.
"""



# ─────────────────────────────────────────────────────────────────────────────
#  终止守卫 (多层判定)
# ─────────────────────────────────────────────────────────────────────────────

@dataclass
class TerminationGuard:
    """
    五层终止判定守卫。任意一层触发 → 返回 (True, reason)。

    Layer 1  LLM 自我声明      <CEREBRUM_COMPLETE/>              (外部已解析，不在此)
    Layer 2  假设树收敛        所有假设 = 终态 + 所有队列空 + 无活跃 Drone
    Layer 3  停滞检测          连续 stagnation_rounds 轮无新 Finding 确认
    Layer 4  预算守卫          超 max_rounds / max_tasks / max_wall_time
    Layer 5  硬性安全网        由外部 asyncio.wait_for 保证，不在此实现
    """
    max_rounds:        int   = 30      # 最多推理轮数
    max_tasks:         int   = 200     # 最多派发任务数
    max_wall_time:     float = 7200.0  # 最长运行时间（秒）
    stagnation_rounds: int   = 3       # 连续 N 轮无新 Finding 即判定停滞

    # 内部状态
    _start_time:       float = field(default_factory=time.monotonic, init=False)
    _prev_hypotheses:  int   = field(default=0, init=False)
    _prev_findings:    int   = field(default=0, init=False)
    _stagnant_count:   int   = field(default=0, init=False)

    def check(
        self,
        round_num:      int,
        blackboard:     Blackboard,
        active_drones:  int,
        total_tasks:    int,
    ) -> tuple[bool, str]:
        """
        综合检查所有终止条件。
        返回 (should_stop, reason_string)。
        reason_string 为空字符串表示继续运行。
        """
        stats = blackboard.stats()
        elapsed = time.monotonic() - self._start_time

        # ── Layer 4: 预算守卫 ────────────────────────────────────────────────
        if round_num >= self.max_rounds:
            return True, f"budget:max_rounds={self.max_rounds}"

        if total_tasks >= self.max_tasks:
            return True, f"budget:max_tasks={self.max_tasks}"

        if elapsed >= self.max_wall_time:
            return True, f"budget:max_wall_time={self.max_wall_time:.0f}s"

        # ── Layer 2: 假设树收敛 ──────────────────────────────────────────────
        # 条件：所有假设都到达终态 + 队列空 + 无活跃 Drone
        total_h      = stats["hypotheses"]
        terminal_h   = stats["confirmed"] + stats["discarded"]
        queues_empty = (
            blackboard.get_active_tasks() == [] and
            active_drones == 0
        )

        if total_h > 0 and terminal_h == total_h and queues_empty:
            return True, f"converged:all_{total_h}_hypotheses_terminal"

        # ── Layer 3: 停滞检测 ────────────────────────────────────────────────
        # P4 修复v2: 仅在所有 Drone 都完成后才计入停滞轮数。
        # 如果仍有活跃 Drone 在执行，说明证据还在收集中，不算停滞。
        cur_f = stats["findings"] + stats["confirmed"]

        if active_drones > 0 or len(blackboard.get_active_tasks()) > 0:
            # 仍有工作在进行中，暂不计入停滞
            pass
        elif cur_f == self._prev_findings:
            self._stagnant_count += 1
        else:
            self._stagnant_count = 0   # 有新发现，重置

        self._prev_findings = cur_f

        if self._stagnant_count >= self.stagnation_rounds:
            return True, (
                f"stagnation:no_new_findings_for_{self.stagnation_rounds}_rounds"
                f"(findings={cur_f})"
            )

        return False, ""

    def budget_summary(self, round_num: int, total_tasks: int) -> str:
        """返回当前预算消耗的简要描述，供 Prompt 注入。"""
        elapsed  = time.monotonic() - self._start_time
        rem_r    = self.max_rounds - round_num
        rem_t    = self.max_tasks  - total_tasks
        rem_w    = self.max_wall_time - elapsed
        stag_rem = self.stagnation_rounds - self._stagnant_count
        return (
            f"Remaining budget: {rem_r} rounds | {rem_t} tasks | "
            f"{rem_w/60:.1f} min | stagnation counter: {self._stagnant_count}/{self.stagnation_rounds}"
            f" (must produce new hypotheses or findings within {stag_rem} round(s) or terminate)"
        )


# ─────────────────────────────────────────────────────────────────────────────
#  Cerebrum Class
# ─────────────────────────────────────────────────────────────────────────────

class Cerebrum:
    def __init__(
        self,
        blackboard:            Blackboard,
        work_dir:              Path,
        root_dir:              Path,
        config:                Any,
        max_concurrent_drones: int   = 3,
        max_rounds:            int   = 30,
        max_tasks:             int   = 200,
        max_wall_time:         float = 7200.0,
        stagnation_rounds:     int   = 3,
        sector_context:        Optional[SectorContext] = None,
        telemetry_queue:       Optional[asyncio.Queue] = None,
    ):
        self.blackboard            = blackboard
        self.work_dir              = work_dir
        self.root_dir              = root_dir
        self.config                = config
        self.max_concurrent_drones = max_concurrent_drones
        self.sector_context        = sector_context
        self.telemetry: asyncio.Queue = telemetry_queue if telemetry_queue is not None else asyncio.Queue(maxsize=2000)

        self.session:       Any           = None
        self._agent_file:   Optional[Path] = None
        self._running:      bool          = False
        self._round:        int           = 0
        self._total_tasks:  int           = 0        # 已派发任务总计数
        self._bg_tasks:     set           = set()    # 后台 Task 集合，stop() 时取消
        self._doc_intel:    str           = ""       # Round 0 文档情报
        self._findings_history: list[int] = []       # 每轮 finding 数量快照

        # ── 领域驱动管线状态 ──
        self._domain_pipeline: list[DomainPhase] = []  # 领域工作流阶段列表
        self._current_phase_idx: int = 0               # 当前阶段索引
        self._domain_terrain: str = ""                  # SKILL.md 地形知识（每轮注入）
        self._detected_domains: list[str] = []          # 检测到的领域列表

        # 终止守卫
        self._guard = TerminationGuard(
            max_rounds=max_rounds,
            max_tasks=max_tasks,
            max_wall_time=max_wall_time,
            stagnation_rounds=stagnation_rounds,
        )

        # 内部队列（不暴露给外部）
        self._task_queue:   asyncio.Queue = asyncio.Queue()
        self._result_queue: asyncio.Queue = asyncio.Queue()
        self._active_drones: int          = 0

        # Bug Fix: telemetry 队列加上 maxsize，防止 OOM。
        pass

    # ─── 公共接口 ─────────────────────────────────────────────────────────────

    async def launch(self, target: str):
        """启动自主分析循环。阻塞直到分析完成或 stop() 被调用。"""
        self.blackboard.target = target
        self.blackboard.active = True
        self._running = True
        self._findings_history = []
        self._doc_intel = ""

        await self._setup_session()
        await self._emit("cerebrum_started", {"target": target})

        # ── Sector 模式下跳过侦察和文档情报（由 Coordinator 已完成）──
        if self.sector_context:
            # Sector 模式: 生成轻量级文件列表作为 doc_intel（不调用 LLM）
            sector = self.sector_context.sector
            self._doc_intel = self._build_sector_file_listing(self.work_dir, sector)
            self._detected_domains = self._detect_domains_from_intel(self._doc_intel)
            self._domain_terrain = self._load_domain_terrain(self._detected_domains)
            self._domain_pipeline = self._build_domain_pipeline(self._detected_domains)
            self._current_phase_idx = 0

            print(f"  🔬 Sector Mini-Cerebrum: {sector.name} ({sector.path})")

            dispatcher_task = asyncio.create_task(self._drone_dispatcher())
            integrator_task = asyncio.create_task(self._result_integrator())
            self._bg_tasks = {dispatcher_task, integrator_task}

            try:
                await self._cerebrum_loop()
            finally:
                try:
                    await asyncio.wait_for(self._drain_active_drones(), timeout=120.0)
                except asyncio.TimeoutError:
                    pass
                await asyncio.sleep(2.0)
                try:
                    await asyncio.wait_for(self._drain_result_queue(), timeout=10.0)
                except (asyncio.TimeoutError, Exception):
                    pass
                tasks_to_cancel = list(self._bg_tasks)
                for t in tasks_to_cancel:
                    t.cancel()
                await asyncio.gather(*tasks_to_cancel, return_exceptions=True)
                await self._sweep_orphaned_hypotheses()

                # Bubble up hypotheses and tasks to the parent blackboard for global reporting
                if hasattr(self.blackboard, 'parent'):
                    parent_bb = getattr(self.blackboard, 'parent')
                    async with parent_bb._lock:
                        for h_id, h in getattr(self.blackboard, 'hypotheses', {}).items():
                            parent_bb.hypotheses[h_id] = h
                        for t_id, t in getattr(self.blackboard, 'tasks', {}).items():
                            parent_bb.tasks[t_id] = t

                await self._emit("cerebrum_finished", {"sector": sector.name})
                self.blackboard.active = False
                if hasattr(self, 'session') and self.session and hasattr(self.session, 'close'):
                    await self.session.close()
                self._cleanup()
            return  # Sector 模式直接返回

        # ── 非 Sector 模式：检查是否需要 Sector 化 ──
        target_path = Path(target) if not target.startswith("http") else None
        if target_path and target_path.is_dir():
            sector_mgr = SectorManager(
                project_root=target_path,
                blackboard=self.blackboard,
                config=self.config,
                telemetry=self.telemetry,
            )
            if sector_mgr.needs_sector_decomposition():
                print(f"🏗️  LARGE PROJECT DETECTED — switching to Sector-Based Analysis")
                await self._emit("cerebrum_thought", {
                    "text": "⚠️ Large project detected. Activating Sector-Based Architecture for parallel subsystem analysis."
                })
                # 清理当前 session（Coordinator 会为每个 Sector 创建新的）
                self._cleanup()
                await self._launch_coordinated(target_path, sector_mgr)
                return  # Coordinator 模式完成后直接返回

        # ── 中小项目：原有 strategic_recon 逻辑 ──
        if target_path and target_path.is_dir():
            narrowed = await self._strategic_recon(target_path)
            if narrowed:
                self.work_dir = narrowed
                target = str(narrowed)
                self.blackboard.target = target
                print(f"🎯 Strategic Focus: narrowed scope to {narrowed}")

        # ── Round 0: 文档情报收集（在主循环之前） ──
        await self._run_doc_intelligence(target)

        # ── 领域驱动管线构建 ──
        self._detected_domains = self._detect_domains_from_intel(self._doc_intel)
        self._domain_terrain = self._load_domain_terrain(self._detected_domains)
        self._domain_pipeline = self._build_domain_pipeline(self._detected_domains)
        self._current_phase_idx = 0

        if self._domain_pipeline:
            phase_names = [p.name for p in self._domain_pipeline]
            print(f"🧭 Domain Pipeline ({', '.join(self._detected_domains)}): {' → '.join(phase_names)}")
        else:
            print(f"🧭 No domain references found, using generic analysis mode")

        # Bug Fix: asyncio.gather 与三个无限轮询循环会相互阻塞。
        # 改为将 dispatcher 和 integrator 作为后台 Task，
        # cerebrum_loop 结束后主动 cancel 它们。
        dispatcher_task  = asyncio.create_task(self._drone_dispatcher())
        integrator_task  = asyncio.create_task(self._result_integrator())
        self._bg_tasks   = {dispatcher_task, integrator_task}

        try:
            await self._cerebrum_loop()
        finally:
            # P5 修复: Cerebrum 退出后，先等活跃 Drone 结束，
            # 再让 result_integrator 把最后的结果回写黑板，
            # 最后才 cancel 后台协程。
            try:
                await asyncio.wait_for(
                    self._drain_active_drones(), timeout=120.0
                )
            except asyncio.TimeoutError:
                pass

            # 给 integrator 最后 10 秒处理剩余结果
            await asyncio.sleep(2.0)
            try:
                await asyncio.wait_for(
                    self._drain_result_queue(), timeout=10.0
                )
            except (asyncio.TimeoutError, Exception):
                pass

            # 停止后台协程时，必须使用 list(self._bg_tasks) 进行快照。
            # 避免在 cancel 或 await 期间因任务完成触发 discard，导致集合大小改变报错 RuntimeError，
            # 从而跳过 gather 直接强制退出引发 Task destroyed pending 报错。
            tasks_to_cancel = list(self._bg_tasks)
            for t in tasks_to_cancel:
                t.cancel()
            await asyncio.gather(*tasks_to_cancel, return_exceptions=True)

            # ── P0 FIX: 假设 → Finding 收割补全 ──────────────────────────────
            # 扫描所有 CONFIRMED/SUSPECTED 假设，补全缺失的 Finding。
            # 修复根因：Critic 确认了假设但 Drone 输出解析未触发 add_finding()
            await self._sweep_orphaned_hypotheses()

            await self._emit("cerebrum_finished", {})
            self.blackboard.active = False
            
            if hasattr(self, 'session') and self.session and hasattr(self.session, 'close'):
                await self.session.close()
            
            # [KILL LEAKED SDK TASKS]
            # 第三方 SDK（如 kimi_agent_sdk 的 SSE reader）可能会在 Timeout 时遗留僵尸任务 (Event.wait)
            current = asyncio.current_task()
            all_tasks = asyncio.all_tasks()
            rogue_tasks = [t for t in all_tasks if t is not current and not t.done()]
            if rogue_tasks:
                for t in rogue_tasks:
                    t.cancel()
                try:
                    await asyncio.wait_for(asyncio.gather(*rogue_tasks, return_exceptions=True), timeout=2.0)
                except asyncio.TimeoutError:
                    pass

            self._cleanup()

    async def _drain_active_drones(self):
        """等待所有活跃 Drone 完成。"""
        while self._active_drones > 0:
            await asyncio.sleep(1.0)

    async def _drain_result_queue(self):
        """P5: 修复结果队列清理漏洞，改为等待 integrator 将其消化完毕，而不是主动丢弃。"""
        while not self._result_queue.empty():
            await asyncio.sleep(0.5)

    async def _sweep_orphaned_hypotheses(self):
        """P0 FIX: 假设 → Finding 最终收割机制。

        多层收割策略：
        Layer 1: 扫描 CONFIRMED/SUSPECTED 假设 (confidence >= 0.5)，有证据链 → 提升为 Finding
        Layer 2: 扫描所有 ACTIVE/PENDING 假设 (confidence >= 0.85)  → 强制提升为 Finding
                 根因: 大模型 Critic 流程在 sector mode 下经常因 budget/timeout 被中断，
                 导致高置信假设永远停留在 ACTIVE 状态，不会被标记为 CONFIRMED。
        """
        # 收集已有 Finding 的假设 ID
        existing_finding_hyp_ids = {f.hypothesis_id for f in self.blackboard.findings}

        promoted_count = 0
        for h_id, h in self.blackboard.hypotheses.items():
            if h_id in existing_finding_hyp_ids:
                continue  # 已有 Finding，跳过

            # Layer 1: CONFIRMED/SUSPECTED + confidence >= 0.5
            # Layer 2: ANY status + confidence >= 0.85 (高置信强制收割)
            is_layer1 = (
                h.status in (HypothesisStatus.CONFIRMED, HypothesisStatus.SUSPECTED)
                and h.confidence >= 0.5
            )
            is_layer2 = h.confidence >= 0.85

            if not (is_layer1 or is_layer2):
                continue

            # 从证据链重建 Finding 信息
            severity = "medium"  # 默认 severity
            evidence_parts = []
            critic_notes = []

            for ev in h.evidence:
                if ev.startswith("[confirmed]"):
                    ev_lower = ev.lower()
                    if "critical" in ev_lower:
                        severity = "critical"
                    elif "high" in ev_lower:
                        severity = "high"
                    evidence_parts.append(ev)
                elif ev.startswith("[critic-accepted]"):
                    critic_notes.append(ev)
                    evidence_parts.append(ev)

            # Layer 2 的假设可能没有 [confirmed]/[critic-accepted] 标记，
            # 直接使用假设描述本身作为证据
            if not evidence_parts and is_layer2:
                evidence_parts.append(f"[auto-promoted] Confidence={h.confidence:.0%}: {h.description[:300]}")
                # 根据描述中的关键词推断严重性
                desc_lower = h.description.lower()
                if any(kw in desc_lower for kw in ("command injection", "rce", "remote code", "buffer overflow", "heap overflow")):
                    severity = "high"
                elif any(kw in desc_lower for kw in ("race condition", "toctou", "truncation", "integer overflow")):
                    severity = "medium"
            elif not evidence_parts:
                continue

            title = h.description[:120]
            evidence_text = "\n".join(evidence_parts[:5])

            # 从关联任务中提取更丰富的描述
            task_details = []
            for t_id in h.tasks:
                task = self.blackboard.tasks.get(t_id)
                if task and task.result and task.status == TaskStatus.DONE:
                    # 提取 Drone 结果的前 300 字作为补充描述
                    raw = task.result if isinstance(task.result, str) else str(task.result)
                    task_details.append(raw[:300])

            description = h.description
            if task_details:
                description += "\n\n## Evidence from Drone Investigation\n" + "\n---\n".join(task_details[:3])

            await self.blackboard.add_finding(
                hypothesis_id=h_id,
                title=title,
                description=description,
                severity=severity,
                evidence=evidence_text,
            )
            promoted_count += 1

            await self._emit("finding_promoted_from_hypothesis", {
                "hypothesis_id": h_id,
                "title": title[:80],
                "severity": severity,
            })

        if promoted_count > 0:
            await self._emit("cerebrum_thought", {
                "text": f"✅ Post-loop sweep promoted {promoted_count} orphaned hypothesis(es) to Findings"
            })


    async def stop(self, reason: str = "user_stop"):
        self._running = False
        # Bug Fix: 取消后台 bg_tasks（如果存在）
        for t in getattr(self, "_bg_tasks", set()):
            t.cancel()
        self.blackboard.active = False
        await self._emit("cerebrum_stopped", {"reason": reason})
        self._cleanup()

    # ─── LLM Session 初始化 ───────────────────────────────────────────────────

    def _detect_domains_from_intel(self, doc_intel: str) -> list[str]:
        """根据 Round 0 文档情报分析结果，语义识别匹配的安全领域。

        在 doc-analyst 理解项目之后调用，而非根据文件名猜测。
        使用正则 word-boundary 匹配，避免子串误匹配（如 "rag" 匹配 "storage"）。
        """
        import re as _re
        matched = set()
        text = doc_intel.lower()

        def _has(keywords: list[str]) -> bool:
            """全词匹配：任一关键词在文本中以独立单词出现则返回 True。"""
            return any(_re.search(r'\b' + _re.escape(kw) + r'\b', text) for kw in keywords)

        # ── Web ──
        if _has(["http server", "rest api", "graphql", "api endpoint", "router",
                 "middleware", "flask", "django", "fastapi", "express",
                 "gin-gonic", "echo framework", "spring boot", "trpc",
                 "web server", "web framework", "http handler"]):
            matched.add("web")

        # ── AI Agent ──
        if _has(["llm", "langchain", "openai", "anthropic", "mcp server",
                 "mcp tool", "chatbot", "gpt", "tool_call", "function_call",
                 "rag pipeline", "embedding", "vector database", "langraph",
                 "crewai", "dify", "workflow engine", "ai assistant",
                 "ai agent", "large language model", "prompt injection"]):
            matched.add("ai-agent")

        # ── Binary / Native ──
        if _has(["elf binary", "binary exploitation", "assembly", "disassembly",
                 "buffer overflow", "heap overflow", "stack overflow", "pwn",
                 "firmware", "reverse engineer", "memory corruption",
                 "native code", "c/c++"]):
            matched.add("binary")

        # ── Storage Engine ──
        if _has(["database", "storage engine", "lsm-tree", "lsm tree", "btree",
                 "b-tree", "compaction", "memtable", "sstable", "write-ahead log",
                 "rocksdb", "leveldb", "sqlite", "redis", "kv store",
                 "key-value store", "transaction", "mvcc"]):
            matched.add("storage-engine")

        # ── Data Parser ──
        if _has(["parser", "decoder", "encoder", "codec", "serialization",
                 "deserialization", "marshal", "unmarshal", "json parser",
                 "xml parser", "yaml parser", "protobuf", "msgpack", "avro",
                 "tokenizer", "lexer"]):
            matched.add("data-parser")

        # ── Desktop / Electron ──
        if _has(["electron", "tauri", "chromium embedded", "desktop app",
                 "nwjs", "node-webkit", "renderer process", "main process",
                 "browserwindow", "nodeintegration"]):
            matched.add("desktop")

        # ── Infra / Cloud Native ──
        if _has(["kubernetes", "k8s", "helm chart", "terraform",
                 "docker", "container", "deployment", "ci/cd", "pipeline",
                 "argocd", "tekton", "serviceaccount", "rbac",
                 "cloud native", "k8s operator", "controller-runtime"]):
            matched.add("infra")

        # ── Foundation Library ──
        if _has(["library", "sdk", "utility", "data structure", "algorithm",
                 "基础库", "工具库", "通用库"]):
            matched.add("foundation-lib")

        # ── Supply Chain ──
        if _has(["ci/cd", "build pipeline", "dependency management",
                 "package manager", "npm registry", "pypi", "cargo crate",
                 "go module", "供应链", "supply chain"]):
            matched.add("supply-chain")

        # ── Mail ──
        if _has(["smtp", "imap", "pop3", "email server", "mime",
                 "sendmail", "postfix", "roundcube", "mail server"]):
            matched.add("mail")

        # ── Browser ──
        if _has(["browser engine", "v8 engine", "webkit", "blink engine",
                 "spidermonkey", "rendering engine", "sandbox escape",
                 "dom engine"]):
            matched.add("browser")

        # ── Counter ──
        if _has(["scanner", "security tool", "antivirus", "edr",
                 "hids", "waf", "安全工具", "扫描器"]):
            matched.add("counter")

        # 兜底：如果啥都没匹配到，至少加 foundation-lib
        if not matched:
            matched.add("foundation-lib")

        # security-expert 路由表始终加载
        matched.add("security-expert")

        return sorted(matched)

    def _load_domain_terrain(self, domains: list[str]) -> str:
        """加载指定领域的 SKILL.md 地形知识，每轮注入。"""
        import re as _re
        briefs = []
        skills_path = self.root_dir / "skills"
        for skill_name in domains:
            brief_file = skills_path / skill_name / "SKILL.md"
            if brief_file.exists():
                content = brief_file.read_text(encoding="utf-8", errors="ignore")
                content = _re.sub(r"\A---.*?---\s*", "", content, count=1, flags=_re.DOTALL)
                briefs.append(f"\n<!-- Domain Terrain: {skill_name} -->\n{content.strip()}\n")
        if not briefs:
            return ""
        domains_str = ", ".join(f"`{d}`" for d in domains if d != "security-expert")
        return (
            f"\n\n## Domain Terrain Intelligence (Detected: {domains_str})\n"
            + "\n".join(briefs)
        )

    def _build_domain_pipeline(self, domains: list[str]) -> list[DomainPhase]:
        """从 skills/*/references/ 加载领域工作流，构建阶段管线。

        每个阶段对应 PHASE_ORDER 中的一个名字（如 "target-definer"）。
        对于每个阶段，合并所有匹配领域的对应 references/*.md 文件。
        如果没有任何领域提供 references/ 目录，返回空列表（退化为通用模式）。
        """
        import re as _re
        skills_path = self.root_dir / "skills"

        # 收集所有有 references/ 目录的领域
        active_domains = []
        for d in domains:
            refs_dir = skills_path / d / "references"
            if refs_dir.is_dir():
                active_domains.append(d)

        if not active_domains:
            return []

        pipeline: list[DomainPhase] = []
        for idx, phase_name in enumerate(PHASE_ORDER):
            # 合并所有领域的同名 reference 文件
            merged_instructions: list[str] = []
            for domain in active_domains:
                ref_file = skills_path / domain / "references" / f"{phase_name}.md"
                if ref_file.exists():
                    content = ref_file.read_text(encoding="utf-8", errors="ignore")
                    content = _re.sub(r"\A---.*?---\s*", "", content, count=1, flags=_re.DOTALL)
                    merged_instructions.append(
                        f"### [{domain.upper()}] {phase_name}\n{content.strip()}\n"
                    )

            if merged_instructions:
                pipeline.append(DomainPhase(
                    name=phase_name,
                    instructions="\n".join(merged_instructions),
                    order=idx,
                    max_rounds=PHASE_MAX_ROUNDS.get(phase_name, 3),
                ))

        return pipeline

    async def _setup_session(self):
        from kimi_agent_sdk import Session
        from kaos.path import KaosPath as KP

        bots_md = self.root_dir / ".bots.md"
        base = bots_md.read_text(encoding="utf-8", errors="ignore") if bots_md.exists() else ""

        # ── Phase 1: 只加载 security-expert 路由表 ──
        # 领域简报在 Round 0 理解项目后再动态注入（见 launch() 中的 Phase 2）
        routing_brief = ""
        routing_file = self.root_dir / "skills" / "security-expert" / "SKILL.md"
        if routing_file.exists():
            import re as _re
            content = routing_file.read_text(encoding="utf-8", errors="ignore")
            content = _re.sub(r"\A---.*?---\s*", "", content, count=1, flags=_re.DOTALL)
            routing_brief = (
                "\n\n## Security Domain Routing Table\n"
                "Use this to identify which domains match the project in Round 0.\n"
                f"{content.strip()}\n"
            )

        # ── Sector 模式使用精简 prompt ──
        if self.sector_context:
            sector = self.sector_context.sector
            sector_prompt = SECTOR_CEREBRUM_PROMPT.format(
                sector_path=sector.path,
                sector_name=sector.name,
                sector_description=sector.description,
                sector_attack_surface=sector.attack_surface,
            )
            instructions = f"{base}\n\n{sector_prompt}"
        else:
            instructions = f"{base}\n\n{CEREBRUM_PROMPT}{routing_brief}"

        tmp_dir = self.root_dir / "tmp"
        tmp_dir.mkdir(parents=True, exist_ok=True)
        
        # Write instructions to a system prompt file for the SDK
        self._sys_prompt_file = tmp_dir / f"cerebrum_prompt_{uuid.uuid4().hex[:8]}.md"
        self._sys_prompt_file.write_text(instructions, encoding="utf-8")

        spec = {
            "version": 1, 
            "agent": {
                "name": "cerebrum",
                "system_prompt_path": str(self._sys_prompt_file.absolute()),
                "tools": []  # CRITICAL: Strip all tools from Cerebrum
            }
        }
        self._agent_file = tmp_dir / f"cerebrum_{uuid.uuid4().hex[:8]}.yaml"
        self._agent_file.write_text(yaml.dump(spec, allow_unicode=True), encoding="utf-8")

        # skills_dir=None：不通过 SDK 加载 skills/，避免误加载 MCP 声明
        # （kimisec-hive 会导致递归实例化，scheduler/find-skills 与安全审计无关）
        self.session = await Session.create(
            work_dir=KP(str(self.work_dir)),
            yolo=True,
            agent_file=self._agent_file,
            skills_dir=None,
            config=self.config,
        )

        # ── 方案 A: 注入 response_format 到底层 chat provider ──────────────
        # 链路: Session._cli._runtime.llm → Kimi chat provider
        # Kimi.generate() 内部用 **generation_kwargs 展开传给 OpenAI client，
        # 所以 response_format 会被透传到 chat.completions.create()。
        # 这样大模型被物理约束为只能输出合法 JSON，彻底消除思想泄漏问题。
        try:
            cli = getattr(self.session, '_cli', None)
            runtime = getattr(cli, '_runtime', None) if cli else None
            llm = getattr(runtime, 'llm', None) if runtime else None
            # LLM 是包装器，真正的 Kimi chat provider 在 llm.chat_provider 上
            chat_provider = getattr(llm, 'chat_provider', None) if llm else None
            if chat_provider and hasattr(chat_provider, 'with_generation_kwargs'):
                new_provider = chat_provider.with_generation_kwargs(
                    response_format={"type": "json_object"}
                )
                llm.chat_provider = new_provider
                await self._emit("cerebrum_thought", {
                    "text": "✅ response_format=json_object injected into chat provider"
                })
            else:
                await self._emit("cerebrum_thought", {
                    "text": "⚠️ Could not inject response_format — SDK structure mismatch, falling back to prompt-only JSON"
                })
        except Exception as e:
            await self._emit("cerebrum_thought", {
                "text": f"⚠️ response_format injection failed: {e} — falling back to prompt-only JSON"
            })

    def _cleanup(self):
        if hasattr(self, '_agent_file') and self._agent_file and self._agent_file.exists():
            try:
                self._agent_file.unlink()
            except Exception:
                pass
        if hasattr(self, '_sys_prompt_file') and self._sys_prompt_file and self._sys_prompt_file.exists():
            try:
                self._sys_prompt_file.unlink()
            except Exception:
                pass

    # ─── Round -1: 战略侦察（超大代码库自动聚焦）──────────────────────────────

    # 超大代码库阈值
    _LARGE_PROJECT_FILE_THRESHOLD = 5000     # 文件数
    _LARGE_PROJECT_SIZE_THRESHOLD = 50_000_000  # 50 MB 源代码

    async def _strategic_recon(self, target_path: Path) -> Optional[Path]:
        """
        Round -1: 战略侦察。

        对于 Chromium、Linux Kernel 等超大型项目，在任何分析之前：
        1. 快速统计项目规模（纯 I/O，不用 LLM）
        2. 如果超过阈值 → 派遣一个战略 Drone 读取顶层架构文档
        3. Drone 返回推荐的高价值子系统路径
        4. Cerebrum 将 work_dir 收窄到该子系统

        人类专家也是这么做的：没有人"分析 Chrome"，他们分析的是"V8 的 JIT 编译器"。
        """
        import os

        # ── Step 1: 快速规模评估（纯 I/O）──
        file_count = 0
        total_size = 0
        top_dirs: dict[str, int] = {}  # 顶层子目录 → 文件数

        try:
            for root, dirs, files in os.walk(str(target_path)):
                # 跳过版本控制和构建产物
                dirs[:] = [d for d in dirs if d not in (
                    '.git', '.svn', 'node_modules', '__pycache__', '.tox',
                    'build', 'out', 'dist', 'target', 'vendor', 'third_party',
                    '.gradle', '.idea', '.vscode',
                )]
                file_count += len(files)
                for f in files:
                    try:
                        total_size += os.path.getsize(os.path.join(root, f))
                    except OSError:
                        pass

                # 统计顶层子目录的文件分布
                rel = os.path.relpath(root, str(target_path))
                top_dir = rel.split(os.sep)[0] if os.sep in rel else rel
                if top_dir != '.':
                    top_dirs[top_dir] = top_dirs.get(top_dir, 0) + len(files)

                # 快速截断：如果已经确认是超大项目，不需要精确计数
                if file_count > self._LARGE_PROJECT_FILE_THRESHOLD * 2:
                    break
        except Exception:
            return None

        # ── Step 2: 判断是否需要战略聚焦 ──
        is_large = (
            file_count > self._LARGE_PROJECT_FILE_THRESHOLD
            or total_size > self._LARGE_PROJECT_SIZE_THRESHOLD
        )

        if not is_large:
            return None  # 项目规模可控，不需要聚焦

        await self._emit("cerebrum_thought", {
            "text": (
                f"⚠️ ULTRA-LARGE PROJECT DETECTED: ~{file_count} files, "
                f"~{total_size // 1_000_000}MB. Initiating strategic recon to select "
                f"highest-value attack subsystem..."
            )
        })

        # 构建目录概览
        sorted_dirs = sorted(top_dirs.items(), key=lambda x: x[1], reverse=True)[:20]
        dir_overview = "\n".join(
            f"  {d}: ~{c} files" for d, c in sorted_dirs
        )

        # ── Step 3: 派遣战略 Drone ──
        recon_task = (
            f"This is an ULTRA-LARGE codebase (~{file_count} files, ~{total_size // 1_000_000}MB).\n"
            f"Located at: {target_path}\n\n"
            f"Top-level directory structure (by file count):\n{dir_overview}\n\n"
            f"Your mission:\n"
            f"1. Read ONLY the top-level README.md, ARCHITECTURE.md, or docs/ index\n"
            f"2. Identify the SUBSYSTEM with the highest security value for vulnerability research.\n"
            f"   Prioritize: network-facing parsers, IPC/RPC layers, authentication modules,\n"
            f"   sandbox boundaries, native code that handles untrusted input.\n"
            f"3. Return your recommendation in EXACTLY this format:\n\n"
            f"RECOMMENDED_SUBSYSTEM: <directory name from the list above>\n"
            f"RECOMMENDED_PATH: <relative path from project root, e.g. 'src/net' or 'v8/src/compiler'>\n"
            f"REASONING: <one paragraph explaining why this subsystem is the highest-value target>\n"
            f"ATTACK_SURFACE: <what kind of untrusted input reaches this subsystem>\n\n"
            f"IMPORTANT: You MUST pick exactly ONE subsystem. Do NOT suggest analyzing the whole project.\n"
            f"Do NOT read any source code — only documentation files."
        )

        try:
            drone = Drone(
                task_id="R-1-strategic-recon",
                task_desc=recon_task,
                drone_role="scope-definer",
                work_dir=self.work_dir,
                root_dir=self.root_dir,
                config=self.config,
            )
            await self._emit("drone_launched", {
                "task_id": "R-1-strategic-recon", "role": "scope-definer"
            })

            result = await asyncio.wait_for(drone.execute(), timeout=300.0)

            await self._emit("drone_completed", {"task_id": "R-1-strategic-recon"})
        except asyncio.TimeoutError:
            await self._emit("cerebrum_thought", {
                "text": "Strategic recon timed out after 300s. Falling back to largest source directory."
            })
            result = ""
        except Exception as e:
            import traceback as _tb
            err_detail = f"{type(e).__name__}: {e}" if str(e) else type(e).__name__
            _tb_str = _tb.format_exc()
            import sys as _sys
            _sys.stderr.write(
                f"[Cerebrum] Strategic recon exception:\n{_tb_str}\n"
            )
            _sys.stderr.flush()
            await self._emit("cerebrum_thought", {
                "text": f"Strategic recon failed ({err_detail}). Falling back to largest source directory."
            })
            result = ""

        # ── Step 4: 解析推荐路径 ──
        recommended_path = self._parse_recon_recommendation(result, target_path, sorted_dirs)

        if recommended_path and recommended_path.is_dir():
            await self._emit("cerebrum_thought", {
                "text": f"🎯 Strategic recon selected: {recommended_path.relative_to(target_path)}"
            })
            return recommended_path

        return None

    @staticmethod
    def _parse_recon_recommendation(
        result: str, target_path: Path, sorted_dirs: list[tuple[str, int]]
    ) -> Optional[Path]:
        """从战略 Drone 的输出中解析推荐的子系统路径。"""
        import re

        if not result:
            # Drone 失败时的兜底：选择文件最多的源代码目录（排除 docs/tests/examples）
            skip = {'docs', 'doc', 'test', 'tests', 'examples', 'example', 'scripts',
                     'tools', 'contrib', 'benchmarks', 'samples', '.github'}
            for d, _ in sorted_dirs:
                if d.lower() not in skip:
                    candidate = target_path / d
                    if candidate.is_dir():
                        return candidate
            return None

        # 尝试从输出中提取 RECOMMENDED_PATH
        m = re.search(r'RECOMMENDED_PATH:\s*(.+)', result, re.IGNORECASE)
        if m:
            rel_path = m.group(1).strip().strip("'\"` ")
            candidate = target_path / rel_path
            if candidate.is_dir():
                return candidate

        # 回退：尝试 RECOMMENDED_SUBSYSTEM
        m = re.search(r'RECOMMENDED_SUBSYSTEM:\s*(.+)', result, re.IGNORECASE)
        if m:
            subsys = m.group(1).strip().strip("'\"` ")
            candidate = target_path / subsys
            if candidate.is_dir():
                return candidate

        return None

    def _build_sector_file_listing(self, work_dir: Path, sector: Sector) -> str:
        """为 Sector 生成轻量级的文件树概览，注入作为初始地形知识，避免 LLM 没有上下文直接退出。"""
        import os
        SKIP_DIRS = {".git", "node_modules", "__pycache__", "build", "dist", "vendor", "tests", "test"}

        lines = [f"Sector Directory Overview: {sector.name} ({sector.path})\n"]
        file_count = 0
        try:
            for root, dirs, files in os.walk(str(work_dir)):
                dirs[:] = [d for d in dirs if d not in SKIP_DIRS and not d.startswith('.')]
                rel_root = os.path.relpath(root, str(work_dir))
                if rel_root == ".":
                    rel_root = ""
                else:
                    rel_root += "/"

                for f in files:
                    if f.startswith('.'):
                        continue
                    lines.append(f"- {rel_root}{f}")
                    file_count += 1
                    if file_count >= 500:
                        lines.append("... (more files omitted to save context length)")
                        break
                if file_count >= 500:
                    break
        except Exception as e:
            lines.append(f"(Error reading directory structure: {e})")

        if file_count == 0:
            lines.append("(No source files found in this directory)")

        return "\n".join(lines)

    # ─── Round 0: 文档情报收集 ─────────────────────────────────────────────────

    async def _run_doc_intelligence(self, target: str):
        """
        Round 0: 在主分析循环之前，派遣一个 doc-analyst Drone 扫描项目文档。
        提取安全历史、架构蓝图、API 规范、部署模型等先验知识。
        结果存入 self._doc_intel，在 _build_phase_prompt 中注入。
        """
        await self._emit("cerebrum_round", {"round": 0})
        await self._emit("cerebrum_thought", {
            "text": "Round 0: Dispatching doc-analyst to scan project documentation before code analysis..."
        })

        # 构建文档分析任务描述
        doc_task_desc = (
            f"Analyze all documentation in the project at: {target}\n\n"
            "Scan the following files IN ORDER OF PRIORITY:\n\n"
            "## Tier 1: High Security Value\n"
            "- SECURITY.md / SECURITY_POLICY.md\n"
            "- CHANGELOG.md / HISTORY.md / RELEASES.md (search for: fix, patch, CVE, security, vulnerability)\n"
            "- .github/ISSUE_TEMPLATE/ (bug categories → historical failure modes)\n"
            "- dependabot.yml / renovate.json (supply chain risk awareness)\n\n"
            "## Tier 2: Architecture Understanding\n"
            "- README.md (tech stack, core features, entry points)\n"
            "- ARCHITECTURE.md / DESIGN.md / docs/architecture*\n"
            "- CONTRIBUTING.md (code organization, test strategy)\n"
            "- API.md / docs/api* / openapi.yaml / swagger.json (API endpoint enumeration)\n\n"
            "## Tier 3: Configuration & Deployment\n"
            "- Dockerfile / docker-compose.yml (exposed ports, volumes, privileges)\n"
            "- *.example / *.sample / .env.example (default config values, key structures)\n"
            "- Makefile / Taskfile.yml (build commands revealing internal tools)\n"
            "- .github/workflows/ (CI security scan configs → known weaknesses)\n\n"
            "## Tier 4: Dependencies\n"
            "- package.json / go.mod / requirements.txt / Cargo.toml (dependency versions)\n\n"
            "OUTPUT FORMAT:\n"
            "## 项目概况\n"
            "- 名称: [project name]\n"
            "- 技术栈: [languages/frameworks/databases]\n"
            "- 核心功能: [one-line description]\n\n"
            "## 安全历史信号\n"
            "- [CVE/fix commit]: [what type of issue was fixed]\n\n"
            "## 架构蓝图\n"
            "- 入口点: [router/main files]\n"
            "- 核心模块: [list + responsibilities]\n"
            "- 认证/授权: [declared mechanism — needs verification]\n"
            "- 数据流: [input → processing → storage]\n\n"
            "## 暴露面清单\n"
            "- HTTP 端点: [key routes from docs/OpenAPI]\n"
            "- 外部集成: [3rd party APIs, webhooks]\n"
            "- 管理端/内部接口: [admin routes, diagnostic endpoints]\n\n"
            "## 配置与部署\n"
            "- 默认端口: [list]\n"
            "- 默认凭据/密钥结构: [from .env.example]\n"
            "- 容器权限: [privileged? root? capabilities?]\n\n"
            "## 高价值假设种子\n"
            "Based on documentation intel, these directions merit priority investigation:\n"
            "1. [hypothesis — based on security history]\n"
            "2. [hypothesis — based on architecture attack surface]\n"
            "3. [hypothesis — based on config weakness]\n\n"
            "IMPORTANT: Do NOT analyze code itself. Only read documentation files. "
            "If a file does not exist, note '[NOT FOUND]' and move on."
        )

        try:
            drone = Drone(
                task_id="R0-doc-intel",
                task_desc=doc_task_desc,
                drone_role="doc-analyst",
                work_dir=self.work_dir,
                root_dir=self.root_dir,
                config=self.config,
            )

            await self._emit("drone_launched", {"task_id": "R0-doc-intel", "role": "doc-analyst"})

            # 文档分析用 300s 超时（每个文件读取都需要 API 往返）
            result = await asyncio.wait_for(drone.execute(), timeout=600.0)
            self._doc_intel = result or ""

            await self._emit("drone_completed", {"task_id": "R0-doc-intel"})
            await self._emit("cerebrum_thought", {
                "text": f"Round 0 complete: collected {len(self._doc_intel)} chars of document intelligence."
            })

        except asyncio.TimeoutError:
            self._doc_intel = ""
            await self._emit("drone_timeout", {"task_id": "R0-doc-intel"})
            await self._emit("cerebrum_thought", {
                "text": "Round 0 timed out — proceeding without document intelligence."
            })
        except Exception as e:
            self._doc_intel = ""
            await self._emit("drone_failed", {
                "task_id": "R0-doc-intel",
                "error": f"Doc intelligence failed: {e}"
            })

    # ─── 主脑推理循环 ──────────────────────────────────────────────────────────

    def _current_phase(self) -> Optional[DomainPhase]:
        """获取当前阶段，如无管线则返回 None。"""
        if not self._domain_pipeline:
            return None
        if self._current_phase_idx >= len(self._domain_pipeline):
            return None
        return self._domain_pipeline[self._current_phase_idx]

    def _advance_phase(self) -> bool:
        """推进到下一阶段。返回 True 如果还有后续阶段。"""
        phase = self._current_phase()
        if phase:
            phase.completed = True
        self._current_phase_idx += 1
        if self._current_phase_idx < len(self._domain_pipeline):
            return True
        return False

    async def _cerebrum_loop(self):
        """领域驱动的主脑推理循环。
        
        如果有领域管线: 按阶段顺序推进，每个阶段注入对应的领域方法论。
        如果无领域管线: 使用通用分析模式（仅地形知识）。
        """
        from kimi_agent_sdk import TextPart, ThinkPart, ToolCall, ToolResult

        prompt = self._build_phase_prompt()

        while self._running:
            self._round += 1
            phase = self._current_phase()

            # 更新阶段内轮次计数
            if phase:
                phase.round_count += 1

            phase_label = f" [{phase.name}]" if phase else ""
            await self._emit("cerebrum_round", {
                "round": self._round,
                "phase": phase.name if phase else "generic",
                "phase_round": phase.round_count if phase else self._round,
            })

            full_text = ""
            try:
                # P0 FIX: Sector 模式下不逐 chunk 流式输出 cerebrum_text/thought，
                # 因为多个并行 Mini-Cerebrum 共享同一个 telemetry 队列，
                # 逐 chunk 流式输出会导致不同 sector 的 JSON 内容在终端交错，
                # 产生无法解析的乱码。改为在轮结束后一次性输出摘要。
                is_sector = self.sector_context is not None
                async for chunk in self.session.prompt(prompt):
                    if not self._running:
                        break
                    if isinstance(chunk, ThinkPart):
                        think_text = getattr(chunk, 'text', None) or getattr(chunk, 'content', None) or getattr(chunk, 'thinking', '') or ''
                        if think_text and not is_sector:
                            await self._emit("cerebrum_thought", {"text": str(think_text)})
                    elif isinstance(chunk, TextPart):
                        text_val = getattr(chunk, 'text', None) or getattr(chunk, 'content', '') or ''
                        full_text += str(text_val)
                        if not is_sector:
                            await self._emit("cerebrum_text", {"text": str(text_val)})
                    elif isinstance(chunk, ToolCall):
                        func = getattr(chunk, 'function', None)
                        name = getattr(func, 'name', '?') if func else '?'
                        args = str(getattr(func, 'arguments', ''))[:150] if func else ''
                        await self._emit("tool_call", {"name": name, "args": args})
                    elif isinstance(chunk, ToolResult):
                        rv = getattr(chunk, "return_value", None)
                        out = str(getattr(rv, "output", "")) if rv else ""
                        await self._emit("tool_result", {"preview": out[:200]})
            except asyncio.CancelledError:
                break
            except Exception as e:
                # P1 FIX: 检测 MaxStepsReached 异常并优雅处理
                err_type = type(e).__name__
                if "MaxStepsReached" in err_type or "max" in str(e).lower():
                    await self._emit("cerebrum_thought", {
                        "text": f"⚠️ SDK step limit reached ({e}). "
                                f"Preserving {len(full_text)} chars of partial output."
                    })
                    # 尝试解析已有的 partial 输出
                    if full_text.strip():
                        await self._parse_output(full_text)
                    break
                else:
                    await self._emit("cerebrum_error", {"error": str(e)})
                    break

            # ── 解析 LLM 输出 ─────────────────────────────────────────────
            completed = await self._parse_output(full_text)

            # Sector 模式下在轮结束后输出一行紧凑摘要（替代逐 chunk 流式输出）
            if self.sector_context is not None:
                sector_name = self.sector_context.sector.name
                stats = self.blackboard.stats()
                await self._emit("cerebrum_thought", {
                    "text": f"📊 [{sector_name}] R{self._round} — "
                            f"H:{stats['hypotheses']} F:{stats['findings']} "
                            f"T:{stats['active_tasks']} ({len(full_text)} chars)"
                })

            # ── 阶段推进判定 ──────────────────────────────────────────────
            if phase:
                phase_should_advance = (
                    completed  # LLM 声明完成（is_complete 或 phase_complete）
                    or phase.round_count >= phase.max_rounds  # 超出阶段轮次上限
                )
                if phase_should_advance:
                    has_more = self._advance_phase()
                    next_phase = self._current_phase()
                    if has_more and next_phase:
                        await self._emit("cerebrum_thought", {
                            "text": f"📍 Phase transition: {phase.name} → {next_phase.name}"
                        })
                        # 等待 Drone 结果回来再进入新阶段
                        await self._wait_for_drain(min_wait=3.0)
                        prompt = self._build_phase_prompt()
                        continue
                    else:
                        # 所有阶段完成
                        await self._emit("terminated", {"reason": "pipeline_complete"})
                        await self._emit("cerebrum_complete", {})
                        break
            else:
                # 无管线的通用模式
                if completed:
                    await self._emit("terminated", {"reason": "llm_self_declared"})
                    break

            # ── 守卫检查 ──────────────────────────────────────────────────
            should_stop, reason = self._guard.check(
                round_num=self._round,
                blackboard=self.blackboard,
                active_drones=self._active_drones,
                total_tasks=self._total_tasks,
            )
            if should_stop:
                await self._emit("terminated", {"reason": reason})
                await self._emit("cerebrum_complete", {})
                break

            # 等待 Drone 结果
            await self._wait_for_drain(min_wait=5.0)

            # 构建下一轮 prompt（包含 Blackboard 状态 + 当前阶段指令）
            prompt = self._build_synthesis_prompt()

    async def _wait_for_drain(self, min_wait: float):
        """P2 修复: 等待 Drone 任务消化。
        
        当有活跃 Drone 时，必定等到至少一个 Drone 完成后再继续，
        防止主脑在结果未回来前盲目空转生成假设泡沫。

        P6 修复: 加硬性超时上限，防止 SDK SSE 阻塞导致所有 Drone 永久卡死时
        主脑陷入无限死锁。超时后直接 break，让 Guard 检查决定是否终止。
        """
        MAX_WAIT_SECONDS = 600  # 硬性上限：10 分钟，无论如何都不再等
        await asyncio.sleep(min_wait)

        # 如果有活跃 Drone，等待至少一个完成
        initial_done = len([
            t for t in self.blackboard.snapshot()["tasks"].values()
            if t["status"] in ("done", "failed")
        ])

        waited = min_wait
        while True:
            if self._active_drones == 0:
                break  # 没有活跃 Drone 了，直接继续

            # 硬性超时保护：超过 MAX_WAIT_SECONDS 直接放行
            if waited >= MAX_WAIT_SECONDS:
                import sys as _sys
                _sys.stderr.write(
                    f"[Cerebrum] ⚠️  _wait_for_drain hard timeout ({MAX_WAIT_SECONDS}s) "
                    f"reached with {self._active_drones} active drones — forcing continue\n"
                )
                _sys.stderr.flush()
                break

            # 检查是否有新的完成任务
            current_done = len([
                t for t in self.blackboard.snapshot()["tasks"].values()
                if t["status"] in ("done", "failed")
            ])
            if current_done > initial_done:
                # 至少有一个新 Drone 结果回来了，可以继续
                await asyncio.sleep(2.0)  # 多等 2 秒让更多结果回来
                break

            await asyncio.sleep(2.0)
            waited += 2.0

    # ─── 验证任务构建 ─────────────────────────────────────────────────────────

    def _build_verification_task_desc(self, title: str, description: str, severity: str) -> str:
        """为 Cerebrum 的未验证发现构建 Drone 验证任务描述。"""
        return (
            f"[VERIFICATION REQUIRED] Verify the following unconfirmed finding:\n\n"
            f"Title: {title}\n"
            f"Claimed Severity: {severity}\n"
            f"Description: {description[:500] if description else 'N/A'}\n\n"
            f"You MUST complete ALL of the following verification steps:\n\n"
            f"1. CODE EVIDENCE: Find the exact source code location (file + line number) "
            f"that demonstrates this vulnerability. Quote the relevant code.\n\n"
            f"2. REACHABILITY: Trace the path from an external entry point "
            f"(HTTP handler, main(), CLI arg, public API) to the vulnerable code. "
            f"Is this code reachable by untrusted input?\n\n"
            f"3. EXPLOITABILITY: Can this be triggered with realistic input? "
            f"What preconditions are required (auth, special config, race window)?\n\n"
            f"4. MITIGATION CHECK: Is there upstream validation, sanitization, "
            f"or defense-in-depth that prevents exploitation?\n\n"
            f"5. IMPACT ASSESSMENT: What is the realistic worst-case impact "
            f"given this project's deployment model?\n\n"
            f"If you CONFIRM the finding with concrete evidence:\n"
            f"  FINDING: [specific description with code proof]\n"
            f"  SEVERITY: [adjusted severity based on your verification]\n"
            f"  CONFIDENCE: [0.7+ only if you have direct code evidence]\n"
            f"  EVIDENCE: [exact code snippet with file path and line numbers]\n\n"
            f"If you CANNOT confirm or find the code contradicts the claim:\n"
            f"  FINDING: No anomaly detected\n"
            f"  SEVERITY: none\n"
            f"  EVIDENCE: [what you found that disproves the claim]"
        )

    # ─── 输出解析 ─────────────────────────────────────────────────────────────

    @staticmethod
    def _extract_first_text(xml_block: str, tag_candidates: list[str]) -> str:
        """从 XML 块中按优先级提取第一个匹配子标签的文本内容。"""
        for tag in tag_candidates:
            m = re.search(rf'<{tag}[^>]*>(.*?)</{tag}>', xml_block, re.DOTALL | re.IGNORECASE)
            if m:
                return re.sub(r'<[^>]+>', '', m.group(1)).strip()
        # 没有匹配子标签,取整个块内的纯文本
        clean = re.sub(r'<[^>]+>', '', xml_block).strip()
        return clean[:200] if clean else ''

    async def _parse_output(self, text: str) -> bool:
        """Cerebrum 输出解析器 v2 — JSON-first，XML 兜底。"""
        import json as _json

        if not text.strip():
            return False

        # ── 尝试 JSON 解析 ────────────────────────────────────────────────
        parsed = None
        json_text = text.strip()
        if json_text.startswith('```'):
            json_text = re.sub(r'^```(?:json)?\s*\n?', '', json_text)
            json_text = re.sub(r'\n?```\s*$', '', json_text)
        try:
            parsed = _json.loads(json_text)
        except _json.JSONDecodeError:
            brace = json_text.find('{')
            if brace >= 0:
                depth, end = 0, brace
                for i, ch in enumerate(json_text[brace:], brace):
                    if ch == '{': depth += 1
                    elif ch == '}':
                        depth -= 1
                        if depth == 0: end = i + 1; break
                try:
                    parsed = _json.loads(json_text[brace:end])
                except _json.JSONDecodeError:
                    pass

        if isinstance(parsed, dict):
            return await self._parse_json_output(parsed)

        # ── JSON 失败，回退 XML ───────────────────────────────────────────
        await self._emit("cerebrum_thought", {
            "text": "⚠️ JSON parse failed, falling back to legacy XML regex"
        })
        return await self._parse_xml_output_legacy(text)

    async def _parse_json_output(self, data: dict) -> bool:
        """JSON 结构化输出解析。is_complete 是布尔字段，物理隔离，不受思想泄漏影响。"""
        h_map: dict[str, str] = {}

        for h in data.get("hypotheses", []):
            if not isinstance(h, dict): continue
            claim = str(h.get("claim", "")).strip()
            try:
                conf = min(1.0, max(0.0, float(h.get("confidence", 0))))
            except (ValueError, TypeError):
                conf = 0.5
            if not claim or conf <= 0.15 or claim[:40] in h_map:
                continue
            h_id = await self.blackboard.add_hypothesis(claim, conf)
            h_map[claim[:40]] = h_id
            h_map[claim[:30]] = h_id
            await self._emit("hypothesis_generated", {
                "id": h_id, "claim": claim[:80], "confidence": conf,
            })

        for t in data.get("tasks", []):
            if not isinstance(t, dict): continue
            ref  = str(t.get("hypothesis_ref", "")).strip()
            role = str(t.get("role", "investigator")).strip()
            desc = str(t.get("description", "")).strip()
            if not desc: continue
            if not role: role = "investigator"

            h_id = None
            if ref:
                h_id = h_map.get(ref)
                if not h_id:
                    for key, val in h_map.items():
                        if key.startswith(ref[:20]) or ref[:20].startswith(key[:20]):
                            h_id = val; break
            if not h_id and self.blackboard.hypotheses:
                pending = self.blackboard.get_pending_hypotheses()
                h_id = pending[-1].id if pending else list(self.blackboard.hypotheses.keys())[-1]
            if h_id and desc:
                t_id = await self.blackboard.add_task(h_id, desc, role)
                if t_id:  # 如果被 deduplication 拦截会返回 None
                    await self._task_queue.put(t_id)
                    self._total_tasks += 1
                    await self._emit("task_queued", {"id": t_id, "role": role})
                else:
                    pass # 减少垃圾日志输出

        # ── GC: 限制最大活跃假说数，物理淘汰长尾尾部 ──
        from .blackboard import HypothesisStatus
        active_candidates = [
            h for h in self.blackboard.hypotheses.values() 
            if h.status not in (HypothesisStatus.DISCARDED, HypothesisStatus.CONFIRMED)
        ]
        if len(active_candidates) > 15:
            sorted_candidates = sorted(active_candidates, key=lambda x: x.confidence, reverse=True)
            for h in sorted_candidates[15:]:
                await self.blackboard.update_hypothesis(h.id, status=HypothesisStatus.DISCARDED)

        stats = self.blackboard.stats()
        print(f"  📊 Parser (JSON): {stats['hypotheses']} hypotheses | "
              f"{stats['active_tasks'] + stats.get('done_tasks', 0)} tasks | "
              f"{stats['findings']} findings on Blackboard")

        if data.get("is_complete") is True:
            reason = data.get("complete_reason", "LLM declared analysis complete")
        else:
            reason = "LLM declared analysis complete"

        # P0 修复：发现和证据停滞检测
        findings_count = len(self.blackboard.findings)
        evidence_count = sum(len(h.evidence) for h in self.blackboard.hypotheses.values())
        current_stag_state = (findings_count, evidence_count)

        if not hasattr(self, '_stagnation_state'):
            self._stagnation_state = current_stag_state
            self._stagnation_counter = 0

        if self._stagnation_state == current_stag_state:
            self._stagnation_counter += 1
        else:
            self._stagnation_state = current_stag_state
            self._stagnation_counter = 0

        is_complete = data.get("is_complete") is True

        if self._stagnation_counter >= (self._guard.stagnation_rounds + 1):
            await self._emit("cerebrum_thought", {
                "text": f"⚠️ STAGNATION DETECTED: {self._stagnation_counter} consecutive rounds with no new findings or evidence. Forcing exit check."
            })
            is_complete = True
            reason = f"Stagnated for {self._stagnation_counter} rounds with zero new evidence/findings."

        # ── 领域管线模式下，is_complete 和 phase_complete 都交给 _cerebrum_loop 处理 ──
        # phase_complete=true → 推进到下一阶段
        # is_complete=true → 全部结束
        phase_complete = data.get("phase_complete") is True

        if is_complete or phase_complete:
            if is_complete:
                await self._emit("cerebrum_complete", {
                    "trigger": "llm_declared_complete",
                    "reason": reason,
                })
                self._running = False
            return True

        return await self._check_soft_termination(
            str(data.get("critique", "")) + str(data.get("thinking", ""))
        )

    async def _parse_xml_output_legacy(self, text: str) -> bool:
        """旧版 XML 正则解析器（兜底兼容层）。"""
        text = re.sub(r'```(?:xml|XML)?\s*\n?', '', text)
        text = re.sub(r'```\s*\n?', '', text)
        h_map: dict[str, str] = {}
        CLAIM_TAGS = ['title', 'statement', 'claim', 'name', 'description', 'summary']

        for m in re.finditer(
            r'<(\w+)\s+[^>]*confidence\s*=\s*"([0-9.]+)"[^>]*>(.*?)</\1>',
            text, re.DOTALL | re.IGNORECASE,
        ):
            conf = min(1.0, max(0.0, float(m.group(2))))
            body = m.group(3)
            claim = self._extract_first_text(body, CLAIM_TAGS)
            if not claim or conf <= 0.15 or claim[:40] in h_map: continue
            h_id = await self.blackboard.add_hypothesis(claim, conf)
            h_map[claim[:40]] = h_id
            await self._emit("hypothesis_generated", {"id": h_id, "claim": claim[:80], "confidence": conf})

        for m in re.finditer(r'<task\s+([^>]*?)>(.*?)</task>', text, re.DOTALL | re.IGNORECASE):
            attrs, body = m.group(1), m.group(2)
            task_desc = re.sub(r'<[^>]+>', '', body).strip()
            if not task_desc: continue
            role = "evidence-collector"
            role_m = re.search(r'role\s*=\s*"([^"]*)"', attrs)
            if role_m: role = role_m.group(1)
            h_id = None
            for attr_name in ['hypothesis', 'depends_on', 'hypothesis_ref']:
                ref_m = re.search(rf'{attr_name}\s*=\s*"([^"]*)"', attrs)
                if ref_m and ref_m.group(1).strip():
                    h_id = h_map.get(ref_m.group(1).strip())
                    if not h_id:
                        for key, val in h_map.items():
                            if key.startswith(ref_m.group(1)[:20]): h_id = val; break
                    if h_id: break
            if not h_id and self.blackboard.hypotheses:
                pending = self.blackboard.get_pending_hypotheses()
                h_id = pending[-1].id if pending else list(self.blackboard.hypotheses.keys())[-1]
            if h_id and task_desc:
                t_id = await self.blackboard.add_task(h_id, task_desc, role)
                await self._task_queue.put(t_id)
                self._total_tasks += 1
                await self._emit("task_queued", {"id": t_id, "role": role})

        # 完成信号（带 CRITIQUE 隔离防泄漏）
        text_clean = re.sub(r'<CRITIQUE>.*?</CRITIQUE>', '', text, flags=re.DOTALL | re.IGNORECASE)
        cm = re.search(r'<cerebrum[_-]?complete', text_clean, re.IGNORECASE)
        if cm:
            after = text_clean[cm.end():]
            if not re.search(r'<(?:HYPOTHESIS|TASK)\b', after, re.IGNORECASE):
                await self._emit("cerebrum_complete", {"trigger": "hard_signal_xml_fallback"})
                self._running = False
                return True

        stats = self.blackboard.stats()
        print(f"  📊 Parser (XML fallback): {stats['hypotheses']} hypotheses | "
              f"{stats['active_tasks']} tasks | {stats['findings']} findings")
        return await self._check_soft_termination(text)

    async def _check_soft_termination(self, text: str) -> bool:
        """覆盖率 + 递减收益 + 自然语言措辞三条件终止检测。"""
        stats = self.blackboard.stats()
        total_h = stats["hypotheses"]
        terminal_h = stats["confirmed"] + stats["discarded"]
        non_terminal = total_h - terminal_h
        has_active_work = stats["active_tasks"] > 0 or self._active_drones > 0
        coverage_ratio = terminal_h / total_h if total_h > 0 else 0.0

        findings_this_round = len(self.blackboard.findings)
        if not hasattr(self, '_findings_history'):
            self._findings_history: list[int] = []   # type: ignore[annotation-unchecked]
        self._findings_history.append(findings_this_round)
        stagnant_rounds = 0
        if len(self._findings_history) >= 2:
            for i in range(len(self._findings_history) - 1, 0, -1):
                if self._findings_history[i] == self._findings_history[i - 1]:
                    stagnant_rounds += 1
                else:
                    break

        soft_patterns = [
            r'comprehensive\s+coverage\s+achieved', r'no\s+new\s+hypothes',
            r'analysis\s+is\s+complete',
            r'all\s+hypotheses?\s+(have\s+been\s+)?(exhausted|explored|tested)',
            r'no\s+further\s+(investigation|analysis)\s+(needed|required|warranted)',
        ]
        if any(re.search(pat, text, re.IGNORECASE) for pat in soft_patterns):
            coverage_ok = coverage_ratio >= 0.80 or total_h == 0
            idle_ok = not has_active_work
            exhaustion_ok = non_terminal <= 0 or stagnant_rounds >= 3
            if coverage_ok and idle_ok and exhaustion_ok:
                await self._emit("cerebrum_complete", {
                    "trigger": "soft_signal_validated",
                    "coverage": round(coverage_ratio, 2),
                    "stagnant_rounds": stagnant_rounds,
                    "non_terminal_hypotheses": non_terminal,
                })
                self._running = False
                return True

        return False


    # ─── Prompt 构建 ─────────────────────────────────────────────────────────

    def _build_phase_prompt(self) -> str:
        """构建进入新阶段时的初始 prompt。注入文档情报 + 地形知识 + 阶段指令。"""
        # Round 0 文档情报
        doc_section = ""
        if self._doc_intel:
            doc_section = (
                "\n## 📄 Document Intelligence (Round 0 Pre-Analysis)\n"
                "The following intelligence was extracted from project documentation "
                "BEFORE code analysis. Use it to generate higher-quality initial hypotheses.\n\n"
                f"{self._doc_intel}\n\n"
                "---\n\n"
            )

        # 领域地形知识（SKILL.md）
        terrain_section = self._domain_terrain or ""

        # 当前阶段指令
        phase = self._current_phase()
        if phase:
            phase_header = (
                f"[CEREBRUM — PHASE {phase.order + 1}/{len(self._domain_pipeline)}: "
                f"{phase.name.upper().replace('-', ' ')}]\n"
                f"Target: {self.blackboard.target}\n\n"
            )
            phase_instructions = (
                f"## 🎯 Current Phase: {phase.name}\n"
                f"You are in phase **{phase.name}** of the domain-driven analysis pipeline.\n"
                f"Follow the methodology below to complete this phase's objectives.\n"
                f"Max rounds for this phase: {phase.max_rounds}\n\n"
                f"## Phase Methodology\n"
                f"{phase.instructions}\n\n"
            )
            phase_directive = (
                "Based on the phase methodology above:\n"
                "1. Generate hypotheses specifically aligned with THIS phase's objectives\n"
                "2. Dispatch drone tasks to investigate these hypotheses\n"
                "3. When this phase's objectives are met, set `phase_complete: true`\n"
                "4. If you find high-value findings during any phase, record them immediately\n\n"
            )
        else:
            phase_header = (
                f"[CEREBRUM — ROUND 1: GENERIC ANALYSIS]\n"
                f"Target: {self.blackboard.target}\n\n"
            )
            phase_instructions = ""
            phase_directive = (
                "Begin your analysis. Use document intelligence and domain terrain above.\n"
                "Focus on LOGICAL CONTRADICTIONS, STATE ANOMALIES, "
                "and UNBOUND RESOURCE ACCESS.\n\n"
            )

        return (
            f"{phase_header}"
            f"{doc_section}"
            f"{terrain_section}\n\n"
            f"{phase_instructions}"
            f"{phase_directive}"
            "Output your response as a single valid JSON object with fields: "
            "thinking, hypotheses, tasks, critique, phase_complete (boolean), "
            "is_complete (boolean), complete_reason."
        )

    def _build_synthesis_prompt(self) -> str:
        """构建每轮迭代的 synthesis prompt，包含 Blackboard 状态 + 当前阶段指令。"""
        snap = self.blackboard.snapshot()
        stats = self.blackboard.stats()

        # ── Sector 模式：更紧凑的截断参数 ──
        if self.sector_context:
            max_hypo = self.sector_context.max_hypotheses_in_prompt    # 8
            max_tasks = self.sector_context.max_tasks_in_prompt        # 5
            max_result_chars = self.sector_context.max_drone_result_chars  # 2000
        else:
            max_hypo = 15
            max_tasks = 10
            max_result_chars = 8000

        h_lines = []
        active_hypo = sorted(
            [h for h in snap["hypotheses"].values() if h["status"] != "discarded"],
            key=lambda x: x.get("confidence", 0),
            reverse=True
        )[:max_hypo]
        for h in active_hypo:
            icon = {
                "pending":   "⏳",
                "active":    "🔵",
                "suspected": "🟡",
                "confirmed": "🔴",
            }.get(h["status"], "❓")
            h_lines.append(
                f"  {icon} [{h['id']}] conf={h['confidence']:.2f} "
                f"status={h['status']}: {h['description'][:90]}"
            )

        f_lines = []
        for f in snap["findings"][-5:]:
            f_lines.append(
                f"  🔴 [{f['severity'].upper()}] {f['title']}: {f['description'][:100]}"
            )

        recent_tasks = [
            t for t in sorted(
                snap["tasks"].values(),
                key=lambda x: x.get("completed_at") or 0,
                reverse=True,
            )
            if t["status"] in ("done", "failed") and t.get("result")
        ][:max_tasks]

        task_lines = []
        for t in recent_tasks:
            res = t.get("result", {})
            raw_text = res.get("raw_text", "") if isinstance(res, dict) else str(res)
            preview_len = max_result_chars if len(raw_text) > max_result_chars // 2 else max_result_chars // 2
            if len(raw_text) > preview_len:
                raw_text = raw_text[:preview_len] + f"\n... (truncated {len(raw_text) - preview_len} more chars)"
            task_lines.append(
                f"  [Task {t['id']}] {t.get('description', '')[:100]}\n  Result:\n{raw_text}\n"
            )

        budget_info = self._guard.budget_summary(self._round, self._total_tasks)

        # 当前阶段信息
        phase = self._current_phase()
        if phase:
            phase_context = (
                f"## 🎯 Current Phase: {phase.name} "
                f"(Round {phase.round_count}/{phase.max_rounds})\n"
                f"Follow the phase methodology. Set `phase_complete: true` when objectives are met.\n\n"
                f"## Phase Methodology (reminder)\n"
                f"{phase.instructions}\n\n"
            )
        else:
            phase_context = ""

        anti_laziness_directive = ""
        if phase and phase.name in ["exploit-builder", "poc-generator"]:
            anti_laziness_directive = (
                "**[⚠️ CRITICAL WEAPONIZATION DIRECTIVE ⚠️]**\n"
                "You are in the WEAPONIZATION phase. You MUST dispatch at least one task (role: `exploit-crafter`) to write a complete, locally testable Python/Bash PoC script.\n"
                "Do NOT output an empty tasks array (`\"tasks\": []`)!\n"
                "If the vulnerability requires external infrastructure (e.g. MITM, server compromise), write a standalone script that mocks the required infrastructure locally using Python's http.server or Flask to demonstrate the exploit chain.\n"
                "Generating a standalone PoC script is a STRICT REQUIREMENT. Do not just output a 'conceptual report' and skip execution.\n\n"
            )

        prompt = (
            f"[CEREBRUM — ROUND {self._round}: SYNTHESIS & EXPANSION]\n\n"
            f"## ⏱ Budget Status\n{budget_info}\n\n"
            f"{phase_context}"
            f"## Blackboard State\n"
            f"Active: {stats['active_tasks']} tasks | "
            f"Hypotheses: {stats['hypotheses']} | "
            f"Confirmed: {stats['confirmed']} | "
            f"Discarded: {stats['discarded']}\n\n"
            f"### Hypothesis Tree\n" + ("\n".join(h_lines) or "  (empty)") + "\n\n"
            f"### Confirmed Findings\n" + ("\n".join(f_lines) or "  None yet.") + "\n\n"
            f"### Recent Drone Results\n" + ("\n".join(task_lines) or "  No results yet.") + "\n\n"
            "## Cognitive Triggers (address in your 'thinking' field before generating hypotheses)\n"
            "1. ASSUMPTION AUDIT: Name one assumption about this system that you have NOT yet tested. Why haven't you? Is it because you believe it's safe — and if so, is that belief founded on evidence or habit?\n"
            "2. ANOMALY REFLECTION: In the Drone results above, is there anything SURPRISING — any behavior that contradicts your mental model of the system? Surprises are signposts to 0-days.\n"
            "3. SELF-FALSIFICATION: For your highest-confidence hypothesis, design a test that SHOULD DISPROVE it. If you cannot think of one, your hypothesis may be unfalsifiable and therefore useless.\n\n"
            "Based on your cognitive reflection above:\n"
            "1. Output NEW hypotheses derived from broken assumptions or anomalies — NOT from pattern matching.\n"
            "2. If an existing hypothesis changed (confidence update, new evidence), output it again. DO NOT re-output unchanged hypotheses.\n"
            "3. Dispatch new tasks for PENDING hypotheses. DO NOT repeat previously dispatched tasks.\n"
            "4. Set `phase_complete: true` if phase objectives are met, or you are stuck.\n"
            "5. If all phases exhausted → set is_complete to true with a reason.\n\n"
            f"{anti_laziness_directive}"
            "REMINDER: Output a single valid JSON object. Fields: thinking, hypotheses "
            "(array with claim/target/falsification/confidence), "
            "tasks (array with hypothesis_ref/role/description), critique, "
            "phase_complete (boolean), is_complete (boolean), complete_reason."
        )

        return prompt


    # ─── Drone 调度 ───────────────────────────────────────────────────────────

    async def _drone_dispatcher(self):
        """消费 task_queue，并发启动 Drone workers。"""
        semaphore = asyncio.Semaphore(self.max_concurrent_drones)

        while self._running or not self._task_queue.empty():
            try:
                t_id = await asyncio.wait_for(self._task_queue.get(), timeout=1.0)
            except asyncio.TimeoutError:
                continue
            except asyncio.CancelledError:
                break

            task = self.blackboard.tasks.get(t_id)
            if not task:
                continue

            await self.blackboard.update_task(t_id, status=TaskStatus.RUNNING)
            await self._emit("drone_launched", {"task_id": t_id, "role": task.drone_role})

            # 累加总任务计数（用于预算守卫）
            self._total_tasks += 1

            # Bug Fix: 将 Task 引用存入 _bg_tasks，防止被 GC 回收
            dt = asyncio.create_task(self._run_drone_under_semaphore(semaphore, t_id, task))
            self._bg_tasks.add(dt)
            dt.add_done_callback(self._bg_tasks.discard)

    _DRONE_TIMEOUT = 600          # 外层 Drone 执行超时（秒）
    _DRONE_RETRY_TIMEOUT = 300    # 重试时缩短超时
    _MAX_RETRIES = 1              # 超时最多重试次数

    async def _run_drone_under_semaphore(self, semaphore, t_id: str, task):
        async with semaphore:
            self._active_drones += 1
            try:
                result_text = await self._execute_drone_with_retry(t_id, task)
                await self.blackboard.update_task(t_id, status=TaskStatus.DONE, result=result_text)
                await self._result_queue.put((t_id, result_text))
                await self._emit("drone_completed", {"task_id": t_id})
            except asyncio.TimeoutError:
                # 所有重试耗尽后仍然超时
                partial_result = getattr(self, '_last_drone_error', 'Timed out') or 'Timed out'
                timeout_result = (
                    f"[DRONE TIMEOUT] Task {t_id} timed out after all retries.\n"
                    f"The codebase may be too large for this task's scope.\n"
                    f"Cerebrum should retry with a NARROWER scope — specify exact file paths and line ranges.\n"
                    f"Partial error: {partial_result}"
                )
                await self.blackboard.update_task(t_id, status=TaskStatus.TIMEOUT, result=timeout_result)
                await self._result_queue.put((t_id, timeout_result))
                await self._emit("drone_timeout", {"task_id": t_id})
            except Exception as e:
                import traceback
                tb_str = traceback.format_exc()
                await self.blackboard.update_task(t_id, status=TaskStatus.FAILED, error=f"{e}\n{tb_str}")
                await self._emit("drone_failed", {"task_id": t_id, "error": f"{e}\n{tb_str}"})
            finally:
                self._active_drones -= 1

    async def _execute_drone_with_retry(self, t_id: str, task) -> str:
        """执行 Drone，超时后自动重试一次（缩短超时 + 禁用多轮追踪）。"""
        last_err = None
        for attempt in range(1 + self._MAX_RETRIES):
            timeout = self._DRONE_TIMEOUT if attempt == 0 else self._DRONE_RETRY_TIMEOUT
            try:
                drone = Drone(
                    task_id=t_id,
                    task_desc=task.description,
                    drone_role=task.drone_role,
                    work_dir=self.work_dir,    # Sector 模式下已收窄到 sector.path
                    root_dir=self.root_dir,
                    config=self.config,
                )
                if attempt > 0:
                    # 重试时禁用多轮追踪，只做单轮快速分析
                    drone.MAX_CHASE_ROUNDS = 1
                    await self._emit("cerebrum_thought", {
                        "text": f"🔄 Retrying timed-out drone {t_id} (attempt {attempt + 1}, single-round mode)"
                    })
                result_text = await asyncio.wait_for(drone.execute(), timeout=timeout)
                return result_text
            except asyncio.TimeoutError:
                last_err = getattr(drone, '_error', 'Timed out')
                self._last_drone_error = last_err
                if attempt < self._MAX_RETRIES:
                    await self._emit("cerebrum_thought", {
                        "text": f"⏰ Drone {t_id} timed out after {timeout}s, will retry..."
                    })
                    continue
                raise  # 最后一次重试仍超时，向上抛出

    async def _run_critic(self, task_id: str, drone_parsed: dict, hypothesis) -> tuple[str, str, str]:
        """
        Adversarial Reflection: 拦截 Drone 的发现，扮演防守方架构师进行盘问。
        针对 Kimi 2.5 模型进行了宽松化与推理优化。
        返回 (decision, reasoning, suggested_severity)。
        """
        from kimi_agent_sdk import Session, TextPart
        import re

        finding = drone_parsed.get("finding", "")
        detail = drone_parsed.get("detail", "")
        evidence = drone_parsed.get("evidence", "")
        original_severity = drone_parsed.get("severity", "none")
        hyp_claim = hypothesis.description if hypothesis else "N/A"

        prompt = (
            f"You are the KimiSec CRITIC AGENT (L5 Adversarial Reflection Module).\n"
            f"A Drone researcher claims to have found a vulnerability.\n\n"
            f"## Claim\n"
            f"Hypothesis: {hyp_claim}\n"
            f"Title: {finding}\n"
            f"Severity Claimed: {original_severity}\n"
            f"Description: {detail}\n\n"
            f"## Evidence Provided\n"
            f"```\n{evidence}\n```\n\n"
            f"Your job is to ACT AS A PRAGMATIC DEFENDER. Evaluate if this finding is a complete False Positive.\n"
            f"**Kimi 2.5 Guidelines (Be Lenient but Logical):**\n"
            f"1. **Benefit of the Doubt**: If the logical chain is plausible but missing minor AST/routing links, DO NOT reject it outright. Only reject if it is DEFINITELY impossible.\n"
            f"2. **Test Code Filter**: If the evidence is exclusively located in '*_test.go', 'tests/', or dummy code, you MUST REJECT it.\n"
            f"3. **Admin/Internal APIs**: If the endpoint requires high privileges (Admin/Root), ACCEPT it but downgrade the severity to Medium/Low.\n"
            f"4. **Missing Sanitization**: If data reaches a sink but might be sanitized globally (e.g., ORM, Framework filters), point this out in your reasoning, but you may ACCEPT and downgrade severity if uncertain.\n\n"
            f"Think step-by-step, then output exactly ONE of the following XML blocks:\n"
            f"<CRITIC decision=\"ACCEPT\" severity=\"{original_severity}\"><reason>plausible chain</reason></CRITIC>\n"
            f"<CRITIC decision=\"ACCEPT\" severity=\"low\"><reason>admin only, downgraded</reason></CRITIC>\n"
            f"<CRITIC decision=\"REJECT\" severity=\"none\"><reason>this is local test code</reason></CRITIC>"
        )

        try:
            from kaos.path import KaosPath
            session = await Session.create(
                work_dir=KaosPath(str(self.work_dir)),
                config=self.config,
                yolo=True,
            )

            # P3 FIX: Critic 有 120s 硬性超时，防止大型证据链导致 SDK 步骤爆炸
            async def _collect_critic_output():
                text = ""
                async for chunk in session.prompt(prompt):
                    if isinstance(chunk, TextPart):
                        text += chunk.text
                return text

            try:
                result_text = await asyncio.wait_for(_collect_critic_output(), timeout=120.0)
            except asyncio.TimeoutError:
                # Critic 超时 → 默认 ACCEPT（宁可放行一个 FP，不漏掉 TP）
                return "ACCEPT", f"Critic timed out after 120s — defaulting to ACCEPT", original_severity
            
            m = re.search(r'<CRITIC\s+decision="(ACCEPT|REJECT)"\s+severity="(.*?)">\s*<reason>(.*?)</reason>', result_text, re.DOTALL | re.IGNORECASE)
            if m:
                decision = m.group(1).upper()
                sev = m.group(2).strip().lower()
                reason = m.group(3).strip()
                return decision, reason, sev
            else:
                # P3 FIX: 检查是否有 "Max number of steps" 错误
                if "max number of steps" in result_text.lower() or "step" in result_text.lower() and "limit" in result_text.lower():
                    # 步骤超限 → 默认 ACCEPT，不因引擎限制丢弃发现
                    return "ACCEPT", f"Critic step limit reached — defaulting to ACCEPT. Partial: {result_text[:150]}", original_severity

                # 容错：如果没有标准的 XML 结构，但包含 REJECT 字眼，默认拦截
                if "REJECT" in result_text.upper() and "ACCEPT" not in result_text.upper():
                    return "REJECT", result_text[:200], "none"
                return "ACCEPT", result_text[:200], original_severity
        except Exception as e:
            # P3 FIX: 所有 Critic 异常都默认 ACCEPT — 防止异常导致漏报
            return "ACCEPT", f"Critic Error: {e} — defaulting to ACCEPT", original_severity
        finally:
            if 'session' in locals() and hasattr(session, 'close'):
                await session.close()
    # ─── 结果整合 ─────────────────────────────────────────────────────────────

    async def _result_integrator(self):
        """
        从 result_queue 消费 Drone 结果。
        使用多信号贝叶斯近似更新假设置信度：
          - 发现严重性权重
          - 多 Drone 独立印证加成
          - 负面结果的任务覆盖率感知
          - 超时/失败任务隔离
          - 假设时间衰减
        """
        while self._running or not self._result_queue.empty():
            try:
                t_id, result_text = await asyncio.wait_for(self._result_queue.get(), timeout=1.0)
            except asyncio.TimeoutError:
                continue
            except asyncio.CancelledError:
                break

            task = self.blackboard.tasks.get(t_id)
            if not task:
                continue

            parsed = Drone._parse_round_output(result_text)
            severity = parsed.get("severity", "none")
            drone_confidence = parsed.get("confidence", 0.0)
            if isinstance(drone_confidence, str):
                try:
                    drone_confidence = float(drone_confidence)
                except ValueError:
                    drone_confidence = 0.0

            h = self.blackboard.hypotheses.get(task.hypothesis_id)

            if task.drone_role == "harness-generator":
                safe_task_id = t_id.replace(":", "_")
                job_dir = self.work_dir / "fuzz_jobs" / safe_task_id
                if job_dir.exists() and (job_dir / "build.sh").exists():
                    from .fuzzer import FuzzJob
                    async def run_fuzzer_bg(j_dir, t_src, t_id_in, hyp_id_in):
                        fuzz_job = FuzzJob(j_dir, t_src, timeout_minutes=15)
                        await self._emit("fuzzer_started", {"task_id": t_id_in, "job_dir": str(j_dir)})
                        res = await fuzz_job.run()
                        if res.get("crashes", 0) > 0:
                            await self.blackboard.add_finding(
                                hypothesis_id=hyp_id_in,
                                title=f"Fuzzer discovered {res['crashes']} crashes in {t_id_in}",
                                description=f"ASAN Log:\n```\n{res.get('asan_log', '')[:2500]}\n```",
                                severity="critical",
                                evidence=f"Found {res['crashes']} crash files in {j_dir}/crashes/"
                            )
                            await self._emit("fuzzer_crash_found", {"task_id": t_id_in, "crashes": res["crashes"]})
                        else:
                            await self._emit("fuzzer_finished_clean", {"task_id": t_id_in})
                    ft = asyncio.create_task(run_fuzzer_bg(job_dir, self.root_dir, t_id, task.hypothesis_id))
                    if not hasattr(self, "_bg_tasks"):
                        self._bg_tasks = set()
                    self._bg_tasks.add(ft)
                    ft.add_done_callback(self._bg_tasks.discard)

            if severity not in ("none", ""):
                # ── 正面结果：有实质发现 ──

                # 证据质量软惩罚（不硬拦截，而是降低置信度供 LLM Critic 参考）
                evidence_text = parsed.get("evidence", "")
                evidence_quality_note = ""
                if len(evidence_text.strip()) < 50:
                    evidence_quality_note = " [WARNING: evidence < 50 chars, low quality]"

                # LLM Critic 做最终裁决（它有推理能力处理灰色地带）
                critic_decision, critic_reason, suggested_severity = await self._run_critic(t_id, parsed, h)

                if critic_decision == "ACCEPT":
                    # Kimi 2.5 Critic 允许通过，但可能建议降级
                    final_severity = suggested_severity if suggested_severity in ("critical", "high", "medium", "low") else severity

                    # P1 FIX: Critic reasoning 不再泄露到 Finding description
                    # Description 只包含 Drone 的技术分析，Critic 推理存入 evidence trail
                    finding_desc = parsed.get('detail', '')
                    if not finding_desc.strip():
                        finding_desc = parsed.get('finding', '') or f"Security issue identified in {t_id}"

                    # P2 FIX: Evidence 非空校验 — 没有实证的 Finding 降级为 evidence trail 记录
                    finding_evidence = parsed.get("evidence", "")
                    if not finding_evidence.strip():
                        # 从 Drone 原始输出中尝试恢复代码片段
                        raw = result_text if isinstance(result_text, str) else str(result_text)
                        code_blocks = re.findall(r'```[\s\S]*?```', raw)
                        if code_blocks:
                            finding_evidence = code_blocks[0]
                        else:
                            # 最后兜底：使用原始输出摘录
                            finding_evidence = f"[Source: {t_id}] {raw[:1000]}"

                    await self.blackboard.add_finding(
                        hypothesis_id=task.hypothesis_id,
                        title=parsed.get("finding") or f"Finding from {t_id}",
                        description=finding_desc,
                        severity=final_severity,
                        evidence=finding_evidence,
                    )

                    # Critic reasoning 存入假设证据链，不进入 Finding 正文
                    if h and critic_reason:
                        critic_tag = f"[critic-accepted] {t_id}: {critic_reason[:200]}"
                        if len(h.evidence) < 15:
                            h.evidence.append(critic_tag)

                    await self._emit("finding_confirmed", {
                        "task_id": t_id,
                        "severity": final_severity,
                        "title": parsed.get("finding", ""),
                    })
                    severity = final_severity
                else:
                    await self._emit("finding_rejected_by_critic", {
                        "task_id": t_id,
                        "title": parsed.get("finding", ""),
                        "reason": critic_reason
                    })
                    severity = "none"  # 被反思官枪毙，降级

            if severity not in ("none", ""):
                severity_boost = {
                    "critical": 0.20,
                    "high":     0.15,
                    "medium":   0.10,
                    "low":      0.05,
                }.get(severity.lower(), 0.05)

                corroboration_count = sum(
                    1 for e in h.evidence if e.startswith("[confirmed]")
                )
                corr_bonus = min(0.10, corroboration_count * 0.03)

                # FIX(Root Cause 8): 加权平均替代 max，避免置信度单调递增
                base_conf = min(1.0, max(0.0, drone_confidence))
                blended = 0.6 * h.confidence + 0.4 * base_conf
                new_conf = min(1.0, blended + severity_boost + corr_bonus)

                confirm_tag = f"[confirmed] {t_id}: {severity} {parsed.get('finding', '')[:80]}"
                if len(h.evidence) < 15:
                    h.evidence.append(confirm_tag)

                if new_conf >= 0.85:
                    new_status = HypothesisStatus.CONFIRMED
                elif new_conf >= 0.6:
                    new_status = HypothesisStatus.SUSPECTED
                else:
                    new_status = HypothesisStatus.ACTIVE

                await self.blackboard.update_hypothesis(
                    task.hypothesis_id,
                    confidence=new_conf,
                    status=new_status,
                )

                # 高危发现自动派出 PoC Drone
                if severity in ("critical", "high"):
                    poc_desc = (
                        f"[AUTO-POC] Write a Proof-of-Concept exploit script for this finding:\n"
                        f"Title: {parsed.get('finding', '')}\n"
                        f"Severity: {severity}\n"
                        f"Evidence: {parsed.get('evidence', '')}\n"
                        f"Detail: {parsed.get('detail', '')}\n\n"
                        f"Requirements:\n"
                        f"1. Script must be self-contained and runnable\n"
                        f"2. Save to ./pocs/ directory with a descriptive filename\n"
                        f"3. Include comments explaining each step\n"
                        f"4. Must demonstrate the vulnerability impact\n"
                        f"5. Add safety guards (dry-run mode, confirmation prompts)"
                    )
                    poc_task_id = await self.blackboard.add_task(
                        hypothesis_id=task.hypothesis_id,
                        description=poc_desc,
                        drone_role="evidence-collector",
                    )
                    await self._task_queue.put(poc_task_id)
                    self._total_tasks += 1
                    await self._emit("poc_dispatched", {
                        "task_id": poc_task_id,
                        "finding_title": parsed.get("finding", ""),
                    })

                    # FIX(Root Cause 9): Devil's Advocate Drone — 专职证伪
                    falsify_desc = (
                        f"[DEVIL'S ADVOCATE] Your SOLE OBJECTIVE is to DISPROVE this finding.\n\n"
                        f"Claimed Finding: {parsed.get('finding', '')}\n"
                        f"Claimed Severity: {severity}\n"
                        f"Claimed Evidence: {parsed.get('evidence', '')}\n\n"
                        f"You MUST investigate ALL of the following:\n"
                        f"1. Is there input validation UPSTREAM that prevents malicious input from reaching this code?\n"
                        f"2. Is there a defense layer (WAF, middleware, type system, sandbox) blocking exploitation?\n"
                        f"3. Does the claimed code path actually receive user-controlled input in real deployment?\n"
                        f"4. Is the severity overstated given the project's deployment model (library vs service vs CLI)?\n"
                        f"5. Are there runtime protections (ASLR, stack canary, seccomp) that make exploitation impractical?\n\n"
                        f"If you CANNOT disprove it after thorough investigation:\n"
                        f"  FINDING: Verified - [original title]\n"
                        f"  SEVERITY: [confirmed severity]\n"
                        f"If you CAN disprove it:\n"
                        f"  FINDING: FALSE POSITIVE - [reason]\n"
                        f"  SEVERITY: none\n"
                        f"  EVIDENCE: [specific code/config that disproves the claim]"
                    )
                    falsify_task_id = await self.blackboard.add_task(
                        hypothesis_id=task.hypothesis_id,
                        description=falsify_desc,
                        drone_role="evidence-collector",
                    )
                    await self._task_queue.put(falsify_task_id)
                    self._total_tasks += 1
                    await self._emit("devils_advocate_dispatched", {
                        "task_id": falsify_task_id,
                        "finding_title": parsed.get("finding", ""),
                    })

            else:
                # ── 负面结果：Drone 未发现问题 ──
                if not h:
                    continue

                # 检查任务终态
                task_obj = self.blackboard.tasks.get(t_id)
                task_status = task_obj.status if task_obj else None

                if task_status in (TaskStatus.TIMEOUT, TaskStatus.FAILED):
                    # 超时/失败：不惩罚假设，标记为需要重试
                    retry_tag = f"[retry-needed] {t_id}: {task_status}"
                    if len(h.evidence) < 15:
                        h.evidence.append(retry_tag)
                    await self._emit("task_quarantined", {
                        "task_id": t_id,
                        "hypothesis_id": task.hypothesis_id,
                        "reason": str(task_status),
                    })
                    continue

                # 真实否定结果 → 基于覆盖率的加权惩罚

                # 因子 1：Drone 对自己否定结论的置信度
                drone_neg_conf = min(1.0, max(0.0, drone_confidence))

                # 因子 2：该假设的任务覆盖率（已完成/总任务数）
                total_tasks_for_h = len(h.tasks) if h.tasks else 1
                completed_for_h = sum(
                    1 for tid in h.tasks
                    if self.blackboard.tasks.get(tid) and
                    self.blackboard.tasks[tid].status in (TaskStatus.DONE, TaskStatus.TIMEOUT)
                )
                coverage_factor = completed_for_h / max(1, total_tasks_for_h)

                # 因子 3：连续否定次数（该假设的历史否定占比）
                negative_count = sum(
                    1 for e in h.evidence if e.startswith("[negative]")
                )
                positive_count = sum(
                    1 for e in h.evidence if e.startswith("[confirmed]")
                )
                neg_ratio = negative_count / max(1, negative_count + positive_count)

                # 综合惩罚：三因子加权
                # - 高 Drone 置信度的否定 × 高覆盖率 × 高否定比 → 大幅惩罚
                # - 低 Drone 置信度 或 低覆盖率 → 最小惩罚
                penalty = 0.02 + (drone_neg_conf * 0.05) + (coverage_factor * 0.05) + (neg_ratio * 0.03)
                penalty = min(0.18, penalty)  # 单次最大惩罚不超过 0.18

                new_conf = max(0.0, h.confidence - penalty)

                # 记录否定标记
                neg_tag = f"[negative] {t_id}: drone_conf={drone_neg_conf:.2f}"
                if len(h.evidence) < 15:
                    h.evidence.append(neg_tag)

                # 状态降级
                if new_conf < 0.1:
                    new_status = HypothesisStatus.DISCARDED
                elif new_conf < 0.3 and h.status != HypothesisStatus.CONFIRMED:
                    # 不降级已确认的假设（即使某些后续任务否定）
                    new_status = HypothesisStatus.PENDING
                else:
                    new_status = h.status

                await self.blackboard.update_hypothesis(
                    task.hypothesis_id,
                    confidence=new_conf,
                    status=new_status,
                )

    # ─── 工具 ─────────────────────────────────────────────────────────────────

    async def _emit(self, event: str, data: dict):
        # 1. 本地 telemetry queue（供内部消费者，保留向后兼容）
        try:
            self.telemetry.put_nowait({"event": event, "data": data})
        except asyncio.QueueFull:
            pass
        # 2. 全局事件总线（Redis PubSub → Web 仪表盘 Live Event Stream）
        #    LocalBackend 下此调用为 no-op，不影响单机模式
        self.blackboard.emit_global(event, data)

    # ─── Sector-Based Coordinator ──────────────────────────────────────────────

    async def _launch_coordinated(self, project_root: Path, sector_mgr: SectorManager):
        """
        Coordinator 模式：将大型项目分解为 Sector，并行分析，最后汇聚发现。

        流程：
          Phase 0: Decompose — 用 LLM 将项目分解为 N 个 Sector
          Phase 1: Parallel Sector Analysis — 每个 Sector 独立运行 Mini-Cerebrum
          Phase 2: Cross-Sector Analysis — 从所有 Sector 的 Finding 中识别跨模块利用链
          Phase 3: Report — 生成最终报告
        """
        import time as _time
        coord_start = _time.monotonic()

        await self._emit("coordinator_started", {
            "project": str(project_root),
        })

        # ── Phase 0: Decompose ──
        try:
            sectors = await sector_mgr.decompose()
        except Exception as e:
            await self._emit("cerebrum_thought", {
                "text": f"⚠️ Sector decomposition failed ({e}). Falling back to strategic_recon."
            })
            # 回退到原有 strategic_recon 逻辑
            await self._setup_session()
            narrowed = await self._strategic_recon(project_root)
            if narrowed:
                self.work_dir = narrowed
                self.blackboard.target = str(narrowed)
            await self._run_doc_intelligence(str(self.work_dir))
            self._detected_domains = self._detect_domains_from_intel(self._doc_intel)
            self._domain_terrain = self._load_domain_terrain(self._detected_domains)
            self._domain_pipeline = self._build_domain_pipeline(self._detected_domains)
            self._current_phase_idx = 0
            dispatcher_task = asyncio.create_task(self._drone_dispatcher())
            integrator_task = asyncio.create_task(self._result_integrator())
            self._bg_tasks = {dispatcher_task, integrator_task}
            try:
                await self._cerebrum_loop()
            finally:
                for t in list(self._bg_tasks):
                    t.cancel()
                await asyncio.gather(*list(self._bg_tasks), return_exceptions=True)
                await self._sweep_orphaned_hypotheses()
                await self._emit("cerebrum_finished", {})
                self.blackboard.active = False
                self._cleanup()
            return

        if not sectors:
            await self._emit("cerebrum_thought", {
                "text": "⚠️ Decomposition produced 0 sectors. Falling back to full-project analysis."
            })
            # 回退到原有完整分析流程
            await self._setup_session()
            narrowed = await self._strategic_recon(project_root)
            if narrowed:
                self.work_dir = narrowed
                self.blackboard.target = str(narrowed)
            await self._run_doc_intelligence(str(self.work_dir))
            self._detected_domains = self._detect_domains_from_intel(self._doc_intel)
            self._domain_terrain = self._load_domain_terrain(self._detected_domains)
            self._domain_pipeline = self._build_domain_pipeline(self._detected_domains)
            self._current_phase_idx = 0
            dispatcher_task = asyncio.create_task(self._drone_dispatcher())
            integrator_task = asyncio.create_task(self._result_integrator())
            self._bg_tasks = {dispatcher_task, integrator_task}
            try:
                await self._cerebrum_loop()
            finally:
                for t in list(self._bg_tasks):
                    t.cancel()
                await asyncio.gather(*list(self._bg_tasks), return_exceptions=True)
                await self._sweep_orphaned_hypotheses()
                await self._emit("cerebrum_finished", {})
                self.blackboard.active = False
                self._cleanup()
            return

        print(f"\n{'='*60}")
        print(f"🏗️  SECTOR-BASED ANALYSIS — {len(sectors)} sectors")
        print(f"{'='*60}")
        for i, s in enumerate(sectors):
            print(f"  [{i}] P{s.priority} {s.name} ({s.path}) — {s.attack_surface}")
        print()

        # ── Phase 1: Parallel Sector Analysis ──
        max_concurrent_sectors = min(3, len(sectors))  # 最多 3 个 Sector 并行
        semaphore = asyncio.Semaphore(max_concurrent_sectors)

        async def analyze_one_sector(sector: Sector):
            async with semaphore:
                await self._run_sector_analysis(
                    project_root=project_root,
                    sector=sector,
                    sector_mgr=sector_mgr,
                )

        # 并行启动所有 Sector 分析
        await asyncio.gather(
            *[analyze_one_sector(s) for s in sectors],
            return_exceptions=True,  # 单个 Sector 失败不影响其他
        )

        # ── Phase 2: Cross-Sector Analysis ──
        all_findings = self.blackboard.findings
        if len(all_findings) > 0:
            await self._run_cross_sector_analysis(project_root, all_findings)

        # ── Summary ──
        elapsed = _time.monotonic() - coord_start
        stats = self.blackboard.stats()
        print(f"\n{'='*60}")
        print(f"🏁 SECTOR-BASED ANALYSIS COMPLETE")
        print(f"  Sectors analyzed: {len(sectors)}")
        print(f"  Total findings: {stats['findings']}")
        print(f"  Total hypotheses: {stats['hypotheses']}")
        print(f"  Elapsed: {elapsed:.0f}s")
        print(f"{'='*60}\n")

        await self._emit("coordinator_finished", {
            "sectors": len(sectors),
            "findings": stats["findings"],
            "elapsed_seconds": round(elapsed, 1),
        })

    async def _run_sector_analysis(
        self,
        project_root: Path,
        sector: Sector,
        sector_mgr: SectorManager,
    ):
        """
        对单个 Sector 运行独立的 Mini-Cerebrum 循环。

        关键设计：
          - 独立的 BlackboardPartition（假设/任务隔离）
          - 收窄的 work_dir（只看 sector.path）
          - 动态预算（根据 sector 优先级和文件数量自动调整 max_rounds/max_tasks）
          - 精简的 system prompt（SECTOR_CEREBRUM_PROMPT）
          - Drone 结果截断为 2000 字符（原版 8000）
        """
        import time as _time
        sector.status = "running"
        sector.started_at = _time.time()

        sector_work_dir = sector_mgr.get_sector_work_dir(sector)
        partition = sector_mgr.create_partition(sector)

        # 注意：_emit("sector_analysis_started") 已经触发 TelemetryRenderer 打印启动消息，
        # 此处不再重复 print，避免 "Sector starting" 消息出现两次。
        await self._emit("sector_analysis_started", {
            "sector": sector.name,
            "path": sector.path,
        })

        sector_ctx = SectorContext(
            sector=sector,
            partition=partition,
        )

        # 根据 sector 优先级和文件数量动态调整预算
        # 高优先级 sector（P0/P1）和文件数多的 sector 获得更多预算
        sector_file_count = getattr(sector, 'file_count', 0) or 50
        priority_multiplier = {0: 1.5, 1: 1.2, 2: 1.0, 3: 0.8}.get(sector.priority, 1.0)
        budget_tasks = int(max(40, min(120, sector_file_count * 0.8)) * priority_multiplier)
        budget_rounds = int(max(10, min(25, budget_tasks // 4)))

        # 创建独立的 Mini-Cerebrum 实例
        mini_cerebrum = Cerebrum(
            blackboard=partition,       # 使用隔离的分区视图
            work_dir=sector_work_dir,   # 收窄到 Sector 目录
            root_dir=self.root_dir,     # KimiSec 引擎根目录，用于加载 skills
            config=self.config,
            telemetry_queue=self.telemetry,  # 必须继承遥测队列
            max_concurrent_drones=2,    # Sector 级并发更少
            max_rounds=budget_rounds,   # 动态轮次预算
            max_tasks=budget_tasks,     # 动态任务预算
            max_wall_time=1800.0,       # 30 分钟 / Sector
            stagnation_rounds=4,        # 更宽容的停滞检测
            sector_context=sector_ctx,
        )

        try:
            await mini_cerebrum.launch(str(sector_work_dir))
            sector.status = "done"
            sector.findings_count = len(partition.findings)
        except Exception as e:
            sector.status = "failed"
            sector.error = str(e)
            import traceback
            await self._emit("sector_analysis_failed", {
                "sector": sector.name,
                "error": f"{e}\n{traceback.format_exc()}",
            })
        finally:
            sector.completed_at = _time.time()
            elapsed = sector.completed_at - (sector.started_at or sector.completed_at)
            # 注意：_emit("sector_analysis_completed") 已经触发 TelemetryRenderer 打印完成消息，
            # 此处不再重复 print，避免 "Sector done" 消息出现两次。
            await self._emit("sector_analysis_completed", {
                "sector": sector.name,
                "status": sector.status,
                "findings": sector.findings_count,
                "elapsed_seconds": round(elapsed, 1),
            })

    async def _run_cross_sector_analysis(self, project_root: Path, findings: list):
        """
        Phase 2: 跨 Sector 利用链分析。

        从所有 Sector 的 Finding 中寻找可以组合的跨模块攻击路径。
        例如：Sector A 发现了 SSRF，Sector B 发现了内网服务未鉴权 → 组合为 SSRF→内网未授权访问链。
        """
        if len(findings) < 2:
            return  # 少于 2 个 finding，无法组合

        await self._emit("cross_sector_started", {
            "findings_count": len(findings),
        })

        # 构建 Finding 摘要（不传递原始 Drone 输出，只传标题 + 描述摘要）
        from dataclasses import asdict
        finding_summaries = []
        for f in findings:
            f_dict = asdict(f) if hasattr(f, '__dataclass_fields__') else f
            finding_summaries.append(
                f"- [{f_dict.get('severity', '?').upper()}] "
                f"(sector: {f_dict.get('sector_id', 'unknown')}) "
                f"{f_dict.get('title', 'untitled')}: "
                f"{str(f_dict.get('description', ''))[:200]}"
            )

        cross_prompt = (
            f"You are a CROSS-MODULE SECURITY ANALYST.\n\n"
            f"The following {len(findings)} vulnerabilities were found in SEPARATE modules of a large project:\n\n"
            + "\n".join(finding_summaries) + "\n\n"
            f"Your task:\n"
            f"1. Identify any EXPLOIT CHAINS that combine findings from different modules.\n"
            f"   Example: SSRF in Module A + unauthenticated admin API in Module B = SSRF→Admin Takeover.\n"
            f"2. For each chain, describe the full attack path and impact.\n"
            f"3. If no cross-module chains exist, state so clearly.\n\n"
            f"Output format:\n"
            f"CHAIN: <chain name>\n"
            f"PATH: <step1 (sector A finding) → step2 (sector B finding) → impact>\n"
            f"SEVERITY: <critical/high/medium>\n"
            f"DESCRIPTION: <full attack narrative>\n"
        )

        try:
            drone = Drone(
                task_id="cross-sector-analysis",
                task_desc=cross_prompt,
                drone_role="semantic-analyzer",
                work_dir=project_root,
                root_dir=self.root_dir,  # KimiSec 引擎根目录
                config=self.config,
            )
            result = await asyncio.wait_for(drone.execute(), timeout=300.0)

            # 解析跨模块利用链
            if result:
                import re
                chains = re.findall(
                    r'CHAIN:\s*(.+?)\n.*?SEVERITY:\s*(\w+).*?DESCRIPTION:\s*(.+?)(?=\nCHAIN:|$)',
                    result, re.DOTALL | re.IGNORECASE,
                )
                for chain_name, severity, description in chains:
                    await self.blackboard.add_finding(
                        hypothesis_id="cross-sector",
                        title=f"[Cross-Module] {chain_name.strip()}",
                        description=description.strip()[:2000],
                        severity=severity.strip().lower(),
                        evidence=f"Combined from {len(findings)} sector findings",
                        sector_id="cross-sector",
                    )
                    print(f"  🔗 Cross-sector chain: {chain_name.strip()}")

            await self._emit("cross_sector_completed", {
                "chains_found": len(chains) if result else 0,
            })
        except Exception as e:
            await self._emit("cross_sector_failed", {"error": str(e)})
