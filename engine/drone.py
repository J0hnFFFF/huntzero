"""
Drone Worker — 用完即焚的原子执行节点。
每个 Drone 接收一个极度具体的微任务，
用隔离的轻量 LLM Session 完成后将结果结构化返回。
"""
import asyncio
import re
import uuid
import yaml
from pathlib import Path
from typing import Any, Optional

from kimi_sdk_compat import patch_kimi_agent_sdk


patch_kimi_agent_sdk()


class Drone:
    """
    单个工作蜂节点。
    - 完全无状态，完全隔离
    - 执行完毕即销毁（临时 agent yaml 自动清理）
    - 结果通过 parse_result() 解析为结构化 dict
    """

    def __init__(
        self,
        task_id:    str,
        task_desc:  str,
        drone_role: str,
        work_dir:   Path,
        root_dir:   Path,
        config:     Any,
    ):
        self.task_id    = task_id
        self.task_desc  = task_desc
        self.drone_role = drone_role
        self.root_dir   = root_dir
        self.config     = config
        self._agent_file: Optional[Path] = None

        # work_dir 隔离：每个 Drone 在独立沙盒中工作，防止污染目标仓库
        # ROOT CAUSE FIX: 沙盒必须在目标项目目录之外。
        # 旧方案把沙盒建在 work_dir/.drone_sandboxes/ 里（= 目标项目内），
        # 导致 Kimi SDK session 扫描文件时踩到被并发 _cleanup 删除的目录。
        # 新方案：沙盒放在 root_dir/tmp/.drone_sandboxes/，与目标项目物理隔离。
        self._original_work_dir = work_dir
        sandbox_dir = root_dir / "tmp" / ".drone_sandboxes" / task_id.replace(":", "_")
        sandbox_dir.mkdir(parents=True, exist_ok=True)
        self.work_dir   = sandbox_dir
        self._sandbox   = sandbox_dir

        # 创建指向目标项目的软链接（只读访问）
        project_link = sandbox_dir / "_target"
        if not project_link.exists():
            try:
                project_link.symlink_to(work_dir, target_is_directory=True)
            except (OSError, NotImplementedError):
                # Windows 可能需要管理员权限创建 symlink，回退到直接使用原目录
                self.work_dir = work_dir

    async def _build_session(self):
        from kimi_agent_sdk import Session
        from kaos.path import KaosPath

        role_instructions = (
            f"You are a precision analysis drone. Role: {self.drone_role}.\n"
            "Execute your assigned task with maximum specificity. "
            "Use available tools (file read, shell, search) to gather concrete evidence. "
            "Return only what you can prove with code or data. "
            "Do NOT assume any broader methodology. Follow task instructions strictly."
        )

        bots_md = self.root_dir / ".bots.md"
        base = bots_md.read_text(encoding="utf-8", errors="ignore") if bots_md.exists() else ""
        # 统一底盘防线 (Global Safety & Tool Constraints)
        tool_constraint = (
            "\n\n[HARD CONSTRAINT - SYSTEM SAFETY]\n"
            "1. You are a specialized worker drone. Use the Exploration Channel (grep_search) and Verification Channel (Python AST via bash) to establish concrete proof for your designated task.\n"
            "2. NEVER execute dangerous or destructive exploits (e.g., rm -rf, formatting) that compromise the host.\n"
            "3. If your task requires crafting PoCs, exploit scripts, or report documents, write them into the `./pocs/` subdirectory (create it if it does not exist). "
            "This is the ONLY directory that is guaranteed to be preserved after your session ends. "
            "Writing files to the current working directory root will cause them to be LOST. Examples: `./pocs/run_exploit.sh`, `./pocs/EXPLOIT.md`, `./pocs/poc-rce.js`.\n"
            "4. DO NOT run long-standing web servers or heavy test frameworks unless explicitly instructed.\n"
            "5. Your primary objective is to find concrete evidence. Base your conclusions solely on verifiable code structure.\n"
            "6. DO NOT get stuck in infinite loop tool executions. If a tool command fails 2 times, CHANGE YOUR APPROACH or STOP analysis."
        )

        merged = (
            f"{base}\n\n"
            f"[DRONE ROLE: {self.drone_role}]\n"
            f"{role_instructions}"
            f"{tool_constraint}"
        )

        # 针对特定的 harness-generator 角色，动态挂载该领域的 Fuzz 指导书
        if self.drone_role == "harness-generator":
            harness_guidelies = ""
            for skill_dir in (self.root_dir / "skills").iterdir():
                if skill_dir.is_dir():
                    fuzz_md = skill_dir / "references" / "fuzz-harness.md"
                    if fuzz_md.exists():
                        harness_guidelies += f"\n\n--- Fuzz Harness Guidelines ({skill_dir.name}) ---\n"
                        harness_guidelies += fuzz_md.read_text(encoding="utf-8", errors="ignore")
            
            if harness_guidelies:
                merged += f"\n\n[DOMAIN HARNESS GUIDELINES]\n{harness_guidelies}"

        spec = {"version": 1, "agent": {"extend": "default", "instructions": merged}}
        tmp_dir = self.root_dir / "tmp"
        tmp_dir.mkdir(parents=True, exist_ok=True)
        self._agent_file = tmp_dir / f"drone_{uuid.uuid4().hex[:8]}.yaml"
        self._agent_file.write_text(yaml.dump(spec, allow_unicode=True), encoding="utf-8")

        import sys as _sys

        _sys.stderr.write(f"[Drone {self.task_id}] Creating session (role={self.drone_role})...\n")
        _sys.stderr.flush()

        try:
            session = await asyncio.wait_for(
                Session.create(
                    work_dir=KaosPath(str(self.work_dir)),
                    yolo=True,
                    agent_file=self._agent_file,
                    skills_dir=None,
                    config=self.config,
                ),
                timeout=60.0,  # Session 创建超时 60s
            )
        except asyncio.TimeoutError:
            _sys.stderr.write(f"[Drone {self.task_id}] ❌ Session.create() timed out after 60s (likely API auth/network issue)\n")
            _sys.stderr.flush()
            raise RuntimeError(f"Session.create() timed out for drone {self.task_id} — check API key and network connectivity")
        except Exception as e:
            _sys.stderr.write(f"[Drone {self.task_id}] ❌ Session.create() failed: {e}\n")
            _sys.stderr.flush()
            raise

        _sys.stderr.write(f"[Drone {self.task_id}] ✅ Session created successfully\n")
        _sys.stderr.flush()
        return session

    # 最大自驱追踪轮次
    MAX_CHASE_ROUNDS = 3

    async def execute(self) -> str:
        """
        运行任务并返回文本输出。
        支持多轮自驱追踪，通过结构化证据链管理上下文，防止 token 膨胀。
        """
        from kimi_agent_sdk import TextPart, ThinkPart, ToolCall, ToolResult
        import re as _re
        import time as _time

        session = await self._build_session()
        all_rounds: list[dict] = []  # 结构化的每轮摘要
        final_text = ""

        # 非安全分析角色（scope-definer, doc-analyst）使用透传模式：
        # 直接发送原始任务描述，不包裹安全验证模板，避免输出格式冲突。
        _PASSTHROUGH_ROLES = {"scope-definer", "doc-analyst"}

        if self.drone_role in _PASSTHROUGH_ROLES:
            initial_prompt = (
                f"[DRONE TASK: {self.task_id}]\n"
                f"Role: {self.drone_role}\n\n"
                f"{self.task_desc}"
            )
        else:
            initial_prompt = (
                f"[DRONE TASK: {self.task_id}]\n"
                f"Role: {self.drone_role}\n\n"
                f"Task:\n{self.task_desc}\n\n"
                "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"
                "IMPORTANT: Before reporting any finding, you MUST first answer these\n"
                "screening questions based on your actual code investigation:\n\n"
                "REACHABLE: Can an external/unauthenticated user reach this code path? [yes/no/unknown]\n"
                "EXPLOITABLE: Can this be triggered with realistic, non-contrived input? [yes/no/unknown]\n"
                "MITIGATED: Is there upstream validation or defense that prevents exploitation? [yes/no/unknown]\n\n"
                "Only report a finding if REACHABLE != no AND EXPLOITABLE != no AND MITIGATED != yes.\n"
                "If any screening question indicates the issue is not exploitable, report SEVERITY: none.\n\n"
                "Return your analysis in this exact format:\n\n"
                "REACHABLE: <yes/no/unknown — with brief justification>\n"
                "EXPLOITABLE: <yes/no/unknown — with brief justification>\n"
                "MITIGATED: <yes/no/unknown — with brief justification>\n"
                "FINDING: <one-line summary of what you found>\n"
                "SEVERITY: <critical|high|medium|low|none>\n"
                "CONFIDENCE: <0.0-1.0>\n"
                "EVIDENCE: <concrete code snippet, log line, or data proving the finding>\n"
                "DETAIL: <full technical explanation>\n\n"
                "If nothing significant: FINDING: No anomaly detected  SEVERITY: none\n\n"
                "If you found something but need to trace further, end with:\n"
                "TRACE_NEEDED: <specific question to answer in the next round>\n"
                "TRACE_TARGET: <file path, function name, or code pattern to investigate>"
            )

        # 时间预算：总预算 540s（外层 dispatcher 给 600s，留 60s 余量）
        _TOTAL_BUDGET = 540.0
        _MIN_ROUND_TIMEOUT = 60.0   # 单轮最低超时，低于此值不值得再跑
        _t0 = _time.monotonic()

        try:
            # 透传角色只需单轮执行，不进入多轮追踪
            max_rounds = 1 if self.drone_role in _PASSTHROUGH_ROLES else self.MAX_CHASE_ROUNDS
            for chase_round in range(max_rounds):
                if chase_round == 0:
                    prompt = initial_prompt
                else:
                    prompt = self._build_chase_prompt(all_rounds, chase_round)

                # 动态计算本轮超时：剩余预算平分给剩余轮次
                elapsed = _time.monotonic() - _t0
                remaining_budget = _TOTAL_BUDGET - elapsed
                remaining_rounds = max_rounds - chase_round
                round_timeout = max(remaining_budget / remaining_rounds, _MIN_ROUND_TIMEOUT)

                # 预算耗尽则停止
                if remaining_budget < _MIN_ROUND_TIMEOUT:
                    break

                round_text = ""
                _partial = {"text": ""}  # 可变容器：流式累积，超时时可回收
                try:
                    async def fetch_prompt():
                        async for chunk in session.prompt(prompt):
                            if isinstance(chunk, TextPart):
                                _partial["text"] += chunk.text
                        return _partial["text"]

                    round_text = await asyncio.wait_for(fetch_prompt(), timeout=round_timeout)
                except asyncio.TimeoutError:
                    # 关键修复：超时时保留已流式接收的部分结果，而非丢弃
                    round_text = _partial["text"]
                    self._error = "Drone execution timed out."
                    round_text += (
                        f"\n[TIMEOUT] Drone timed out after 300s. "
                        f"Partial results above ({len(_partial['text'])} chars) are preserved. "
                        f"The task scope may be too broad for this codebase size — "
                        f"consider narrowing to specific file paths and line ranges.\n"
                    )
                    break  # 超时后不再继续追踪
                except Exception as e:
                    # P1 FIX: 捕获 MaxStepsReached 和其他 SDK 异常。
                    # kimi_agent_sdk 在 LLM 陷入工具调用死循环时会抛出
                    # MaxStepsReached('Max number of steps reached: 100')。
                    # 保留已流式接收的部分文本。
                    round_text = _partial.get("text", "") if _partial else ""
                    self._error = str(e)
                    err_type = type(e).__name__
                    if "MaxStepsReached" in err_type or "max" in str(e).lower():
                        round_text += (
                            f"\n[SDK STEP LIMIT] {err_type}: {e}\n"
                            f"The SDK internal step limit was reached. "
                            f"Partial results above ({len(round_text)} chars) are preserved.\n"
                        )
                    else:
                        round_text += f"\n[SYSTEM ERROR] Drone execution caught an exception: {e}\n"
                    break  # SDK 异常后不再继续追踪

                round_parsed = self._parse_round_output(round_text)
                round_parsed["round"] = chase_round
                round_parsed["raw_length"] = len(round_text)
                all_rounds.append(round_parsed)
                final_text = round_text  # 最后一轮的完整输出作为最终结果

                # 每轮立刻抽取可能产生的 PoC（防止后续轮次将其舍弃）
                self._extract_pocs_from_text(round_text, round_idx=chase_round)

                # 多条件决策是否继续追踪
                if chase_round < self.MAX_CHASE_ROUNDS - 1:
                    chase_decision = self._evaluate_chase_decision(all_rounds)
                    if chase_decision == "continue":
                        continue
                    elif chase_decision == "stop":
                        break
                else:
                    break  # 达到上限
        finally:
            if 'session' in locals() and hasattr(session, 'close'):
                await session.close()
            self._cleanup()
        # 如果有多轮，构建最终整合输出
        if len(all_rounds) > 1:
            final_text = self._synthesize_multi_round(all_rounds, final_text)

        return final_text

    def _extract_pocs_from_text(self, text: str, round_idx: int = 0):
        """自动提取大模型输出的 markdown 代码块，并写入 pocs 目录 (或 fuzz_jobs 目录)"""
        if self.drone_role not in ("exploit-crafter", "poc-generator", "harness-generator"):
            return
            
        import re
        # Find all markdown code blocks (handling spaces before lang)
        pattern = r'```([a-zA-Z0-9_+-]+)?[ \t]*\n(.*?)```'
        blocks = re.findall(pattern, text, re.IGNORECASE | re.DOTALL)
        
        if not blocks:
            return
            
        
        dest_dir = self._original_work_dir / ("fuzz_jobs" if self.drone_role == "harness-generator" else "pocs")
        dest_dir.mkdir(parents=True, exist_ok=True)
        
        for i, (lang, content) in enumerate(blocks):
            lang = lang.strip().lower() if lang else "txt"
            ext_map = {
                "python": "py", "javascript": "js", "typescript": "ts", 
                "bash": "sh", "sh": "sh", "json": "json", "html": "html",
                "cpp": "cpp", "c": "c", "java": "java", "ruby": "rb",
                "powershell": "ps1", "markdown": "md", "text": "txt"
            }
            ext = ext_map.get(lang, lang)
            if not ext:
                ext = "txt"
                
            # Filter out tiny snippets (e.g. `ls -la`) unless it's shell
            if len(content.strip()) < 20 and ext not in ("sh", "ps1"):
                continue
                
            safe_task_id = self.task_id.replace(':', '_')
            
            if self.drone_role == "harness-generator":
                job_dir = dest_dir / safe_task_id
                job_dir.mkdir(parents=True, exist_ok=True)
                # Auto naming heuristics for fuzz harness
                if ext in ("c", "cpp"):
                    file_name = "fuzz_harness.c" if ext == "c" else "fuzz_harness.cpp"
                elif ext == "sh":
                    file_name = "build.sh"
                else:
                    file_name = f"r{round_idx}_{i+1}.{ext}"
                target_path = job_dir / file_name
            else:
                # include round_idx to prevent overwriting
                file_name = f"{safe_task_id}_r{round_idx}_poc_{i+1}.{ext}"
                target_path = dest_dir / file_name
                
            target_path.write_text(content.strip() + "\n", encoding="utf-8")

    @staticmethod
    def _parse_round_output(text: str) -> dict:
        """从 Drone 的文本输出中提取结构化字段。"""
        import re as _re
        result: dict = {}
        for field_name, pattern in [
            ("finding",      r'FINDING:\s*(.+?)(?:\n|$)'),
            ("severity",     r'SEVERITY:\s*(\w+)'),
            ("confidence",   r'CONFIDENCE:\s*([0-9.]+)'),
            ("evidence",     r'EVIDENCE:\s*(.+?)(?=\nDETAIL:|\nTRACE_|\Z)'),
            ("detail",       r'DETAIL:\s*(.+?)(?=\nTRACE_|\Z)'),
            ("trace_needed", r'TRACE_NEEDED:\s*(.+?)(?:\n|$)'),
            ("trace_target", r'TRACE_TARGET:\s*(.+?)(?:\n|$)'),
        ]:
            m = _re.search(pattern, text, _re.IGNORECASE | _re.DOTALL)
            if m:
                val = m.group(1).strip()
                if field_name == "confidence":
                    try:
                        result[field_name] = float(val)
                    except ValueError:
                        result[field_name] = 0.0
                else:
                    result[field_name] = val
        return result

    def _evaluate_chase_decision(self, rounds: list[dict]) -> str:
        """
        基于累积证据决定是否继续追踪。
        返回: "continue" | "stop"
        """
        current = rounds[-1]
        severity = current.get("severity", "none").lower()
        confidence = current.get("confidence", 0.0)
        has_trace = bool(current.get("trace_needed"))

        # 条件 1：Drone 明确请求了追踪方向
        if has_trace and severity not in ("none", ""):
            # 但如果置信度在轮次间下降，说明追踪进入了死胡同
            if len(rounds) >= 2:
                prev_conf = rounds[-2].get("confidence", 0.0)
                if isinstance(prev_conf, (int, float)) and isinstance(confidence, (int, float)):
                    if confidence < prev_conf - 0.1:
                        return "stop"  # 置信度显著下降，停止追踪
            return "continue"

        # 条件 2：有中高发现但置信度不够，且本轮没有明确说不需要追踪
        if severity in ("critical", "high", "medium"):
            if isinstance(confidence, (int, float)) and confidence < 0.75:
                # 检查是否没有明确说 TRACE_COMPLETE
                raw_len = current.get("raw_length", 0)
                if raw_len > 100:  # 有实质内容
                    return "continue"

        return "stop"

    def _build_chase_prompt(self, rounds: list[dict], chase_round: int) -> str:
        """
        构建追踪轮次的结构化提示词。
        不是简单拼接旧文本，而是提取关键上下文构建精炼的追踪指令。
        """
        prev = rounds[-1]

        # 提取前轮核心信息（控制 token 量）
        finding_summary = prev.get("finding", "Unknown")[:300]
        evidence_summary = prev.get("evidence", "")[:2000]
        trace_question = prev.get("trace_needed", "")
        trace_target = prev.get("trace_target", "")
        prev_confidence = prev.get("confidence", 0.0)

        # 证据链摘要（所有前轮的关键发现）
        chain_summary = ""
        if len(rounds) > 1:
            chain_lines = []
            for i, r in enumerate(rounds):
                sev = r.get("severity", "?")
                conf = r.get("confidence", "?")
                finding = r.get("finding", "?")
                chain_lines.append(f"  Round {i}: [{sev}] conf={conf} — {finding[:150]}")
            chain_summary = f"\n## Evidence Chain So Far\n" + "\n".join(chain_lines) + "\n"

        # 角色自适应追踪策略
        role_strategy = self._get_role_chase_strategy()

        return (
            f"[DRONE CHASE — Round {chase_round + 1} for {self.task_id}]\n\n"
            f"## Previous Finding\n"
            f"Finding: {finding_summary}\n"
            f"Confidence: {prev_confidence}\n"
            f"Evidence: {evidence_summary}\n"
            f"{chain_summary}\n"
            f"## Trace Directive\n"
            f"Question to answer: {trace_question or 'Deepen the investigation — verify exploitability'}\n"
            f"Target to investigate: {trace_target or 'Upstream callers and input validation'}\n\n"
            f"## Investigation Strategy ({self.drone_role})\n{role_strategy}\n\n"
            "Update your findings with the deepened evidence:\n"
            "FINDING: <updated one-line summary — be more specific than last round>\n"
            "SEVERITY: <critical|high|medium|low|none — adjust based on new evidence>\n"
            "CONFIDENCE: <0.0-1.0 — should change based on what you found>\n"
            "EVIDENCE: <NEW concrete evidence from THIS round's investigation>\n"
            "DETAIL: <complete technical explanation including the full attack chain>\n\n"
            "If you need to go deeper: TRACE_NEEDED: <next question>  TRACE_TARGET: <next target>\n"
            "If investigation is complete: end output normally without TRACE_NEEDED"
        )

    def _get_role_chase_strategy(self) -> str:
        """根据 Drone 角色返回特化的追踪策略（Kimi 2.5 适配：目标驱动，不指定具体工具）。"""
        strategies = {
            "evidence-collector": (
                "1. Verify: Read the exact source code at the evidence location\n"
                "2. Trace callers: Find all call sites of the vulnerable function using the most appropriate method\n"
                "3. Check sanitization: Look for input validation, escaping, or WAF rules upstream\n"
                "4. Assess reachability: Can an unauthenticated external user reach this code path?"
            ),
            "data-flow-tracer": (
                "1. Source identification: Where does the tainted data originate? (HTTP param, file, env)\n"
                "2. Propagation: Trace through each function call — is the data transformed or sanitized?\n"
                "3. Sink confirmation: Does the tainted data reach a dangerous sink without neutralization?\n"
                "4. Gadget chain: If multiple steps, document the complete source→sink path with file:line evidence"
            ),
            "state-validator": (
                "1. State preconditions: What state must the system be in for the bug to trigger?\n"
                "2. Race windows: Is there a TOCTOU gap between check and use?\n"
                "3. Edge conditions: Test boundary values, empty inputs, and type mismatches\n"
                "4. Recovery: What happens after the invariant violation? Does the system detect or recover?"
            ),
            "topology-mapper": (
                "1. Expand the map around the finding location\n"
                "2. Identify all entry points that can reach the vulnerable component\n"
                "3. Check for defense-in-depth: firewalls, middlewares, rate limiters\n"
                "4. Document the shortest path from external input to vulnerability"
            ),
            "exploit-crafter": (
                "1. Craft a functional Proof-of-Concept (PoC) exploit (Python/Bash) reproducing the vulnerability\n"
                "2. The PoC MUST be tested locally in dry-run mode until it works correctly\n"
                "3. Show tangible impact: execute a command, bypass auth, or leak a mocked secret\n"
                "4. If blocked, iterate on the PoC script based on error messages until successful bypass"
            ),
            "harness-generator": (
                "1. Understand the target function signature, data structures, and semantics\n"
                "2. Generate a C/C++ libFuzzer harness (`LLVMFuzzerTestOneInput`) that is hermetic and deterministic\n"
                "3. Write the harness code into a clear markdown code block (lang: c or cpp)\n"
                "4. Write a compile script (build.sh) to build the target and harness with `clang -fsanitize=fuzzer`"
            ),
            "crash-analyzer": (
                "1. Analyze the crash logs and ASan/UBSan backtraces produced by the fuzzer\n"
                "2. Pinpoint the root cause (e.g., Integer Overflow, Out-of-bounds Read/Write, UAF)\n"
                "3. Cross-reference the crashing code path with the source code mapped to the backtrace\n"
                "4. Assess the true exploitability of the crash and construct a confirmed finding"
            ),
        }
        return strategies.get(self.drone_role, (
            "1. Verify the finding with additional evidence\n"
            "2. Trace the data flow upstream and downstream\n"
            "3. Check for existing mitigations\n"
            "4. Assess real-world exploitability"
        ))

    @staticmethod
    def _synthesize_multi_round(rounds: list[dict], last_raw: str) -> str:
        """将多轮追踪的结果整合为单个结构化输出。"""
        # 取最后一轮的结构化字段（最精炼的结论）
        final = rounds[-1]
        # 合并所有轮次的证据
        all_evidence = []
        for i, r in enumerate(rounds):
            ev = r.get("evidence", "")
            if ev:
                all_evidence.append(f"[Round {i}] {ev}")

        # 如果最后一轮有完整的结构化输出，用它作为主体
        # 但追加前几轮的证据作为补充
        if all_evidence and len(rounds) > 1:
            evidence_appendix = (
                "\n\n--- Multi-round evidence chain ---\n" +
                "\n".join(all_evidence)
            )
            return last_raw + evidence_appendix

        return last_raw

    def _cleanup(self):
        # 清理临时 agent yaml
        if self._agent_file and self._agent_file.exists():
            try:
                self._agent_file.unlink()
            except Exception:
                pass

        # 将沙盒产出物持久化到工作区：
        #   ./pocs/  → PoC 脚本、利用代码、exploit 文档
        #   ./reports/ → 报告、证明文档、审计笔记
        # 同时兼容旧行为：沙盒内显式 pocs/ 子目录也一并搬出。
        if hasattr(self, '_sandbox') and self._sandbox and self._sandbox.exists():
            import shutil as _shutil

            # --- 1. 搬运沙盒根目录下的直接文件 ---
            _REPORT_EXTS  = {".md", ".txt", ".rst", ".html", ".pdf"}
            _SCRIPT_EXTS  = {".sh", ".py", ".js", ".ts", ".rb", ".pl", ".ps1"}
            _REPORT_NAMES = {"audit_notes", "logical_proof", "impact_assessment",
                             "exploit_scenario", "final_report", "exploit",
                             "readme", "summary", "findings", "report"}
            try:
                for item in self._sandbox.iterdir():
                    # 跳过 symlink (_target) 和子目录（由步骤 2 处理）
                    if item.name.startswith("_") or item.is_dir():
                        continue
                    ext = item.suffix.lower()
                    stem_lower = item.stem.lstrip(".").lower().replace("-", "_")
                    # 报告类文档 → reports/
                    is_report = (ext in _REPORT_EXTS and
                                 any(kw in stem_lower for kw in _REPORT_NAMES))
                    # 利用脚本 → pocs/（所有 .sh/.py/.js/.ts 及明确命名的 .md）
                    is_exploit = (ext in _SCRIPT_EXTS or
                                  any(kw in stem_lower for kw in
                                      {"exploit", "poc", "payload", "rce", "pwn"}))
                    if is_report:
                        dest_dir = self._original_work_dir / "reports"
                    elif is_exploit:
                        dest_dir = self._original_work_dir / "pocs"
                    else:
                        # 其余未分类文件也保留到 pocs/ 避免丢失
                        dest_dir = self._original_work_dir / "pocs"

                    dest_dir.mkdir(parents=True, exist_ok=True)
                    target = dest_dir / item.name
                    if not target.exists():
                        try:
                            _shutil.copy2(str(item), str(target))
                        except Exception:
                            pass
            except Exception:
                pass

            # --- 2. 搬运沙盒内显式 pocs/ 子目录（兼容旧约定）---
            try:
                sandbox_pocs = self._sandbox / "pocs"
                if sandbox_pocs.exists():
                    dest = self._original_work_dir / "pocs"
                    dest.mkdir(parents=True, exist_ok=True)
                    for f in sandbox_pocs.iterdir():
                        if f.is_file():
                            target = dest / f.name
                            if not target.exists():
                                try:
                                    _shutil.copy2(str(f), str(target))
                                except Exception:
                                    pass
            except Exception:
                pass

        # NOTE: 不在这里删除沙盒目录。
        # shutil.rmtree 会导致 [Errno 2]，因为 Kimi SDK 的后台异步操作
        # 可能仍在引用 work_dir 下的路径。沙盒内容极小（仅一个 symlink），
        # 留到进程结束后由 OS 或下次启动时统一清理。



