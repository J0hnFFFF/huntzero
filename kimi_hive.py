"""
HIVE-MIND INTEL ENGINE [V8.0-HEADLESS]
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

纯 CLI 无 TUI 版本。
- 默认全自动无人值守（--auto-approve 默认开启）
- 分析完成后自动导出报告（--output 默认自动生成路径）
- 支持 JSON / Markdown 两种报告格式
- 分布式改造时直接封装为 Worker 进程，零 UI 耦合

Usage:
    python kimi_hive.py <target>
    python kimi_hive.py https://github.com/owner/repo
    python kimi_hive.py /path/to/local/project
    python kimi_hive.py <target> --workers 8 --work-dir ./workspace --resume
    python kimi_hive.py <target> --no-auto-approve    # 开启人工仲裁
    python kimi_hive.py <target> --output report.json  # 指定报告路径
    python kimi_hive.py <target> --no-report           # 不导出报告
"""

import argparse
import asyncio
import json
import os
import re
import shutil
import sys
import time as _time
from datetime import datetime
from pathlib import Path
from typing import Optional

try:
    from rich.console import Console
    from rich.panel import Panel
    from rich.rule import Rule
    from rich.table import Table
    from rich import box
except ImportError:
    print("\u274c Missing dep: pip install rich")
    sys.exit(1)

# Import compat helpers early so the CPython 3.12.0 frozen+slots dataclass
# workaround is active before kimi_agent_sdk / kimi_cli are loaded.
from kimi_sdk_compat import patch_kimi_agent_sdk

try:
    from kimi_agent_sdk import Config
except ImportError:
    print("❌ Missing dep: pip install kimi-agent-sdk")
    sys.exit(1)

patch_kimi_agent_sdk()

from engine import Blackboard, Cerebrum
from engine.drone import Drone

# ─────────────────────────────────────────────────────────────────────────────
#  全局 Console（stderr 用于日志，stdout 保持干净可 pipe）
# ─────────────────────────────────────────────────────────────────────────────

console = Console(stderr=False, highlight=True)
err     = Console(stderr=True,  highlight=False)   # 调试/错误信息


# ─────────────────────────────────────────────────────────────────────────────
#  辅助：severity 颜色映射
# ─────────────────────────────────────────────────────────────────────────────

SEVERITY_STYLE = {
    "critical": "bold red",
    "high":     "bold orange1",
    "medium":   "bold yellow",
    "low":      "bold blue",
    "none":     "dim",
}

SEVERITY_ICON = {
    "critical": "🔴",
    "high":     "🟠",
    "medium":   "🟡",
    "low":      "🔵",
    "none":     "⚪",
}


# ─────────────────────────────────────────────────────────────────────────────
#  Telemetry 渲染器
# ─────────────────────────────────────────────────────────────────────────────

class TelemetryRenderer:
    """将 Cerebrum telemetry 队列的事件渲染为终端输出。"""

    def __init__(self, blackboard: Blackboard):
        self.bb = blackboard
        self._think_buffer = ""     # 合并连续的 thought 片段再输出
        self._last_was_text = False

    def render(self, event: str, data: dict):
        is_text = (event == "cerebrum_text")
        if getattr(self, "_last_was_text", False) and not is_text:
            console.print()
        self._last_was_text = is_text

        if event == "cerebrum_started":
            console.print(f"\n[bold green]▶ HIVE LAUNCHED[/] → target: [cyan]{data.get('target')}[/]")

        elif event == "cerebrum_round":
            r = data.get("round", "?")
            console.print(Rule(f"[bold cyan] ROUND {r} [/]", style="cyan"))

        elif event == "cerebrum_thought":
            # 思维流：累积后截断显示，避免刷屏
            text = data.get("text", "").strip()
            if text:
                preview = text.replace("\n", " ")[:120]
                console.print(f"  [dim #d2a8ff]🧠 {preview}[/]")

        elif event == "cerebrum_text":
            text = data.get("text", "")
            if text:
                console.print(text, end="", markup=False, style="#e6edf3")

        elif event == "hypothesis_generated":
            h_id = data.get("id", "?")
            claim = data.get("claim", "")[:90]
            conf  = data.get("confidence", 0.0)
            bar   = "█" * int(conf * 10) + "░" * (10 - int(conf * 10))
            console.print(
                f"\n  [bold cyan]🧬 H [{h_id}][/] conf=[green]{bar}[/] {conf:.0%}\n"
                f"     [white]{claim}[/]"
            )

        elif event == "hypothesis_updated":
            h_id   = data.get("id", "?")
            status = data.get("status", "")
            conf   = data.get("confidence", None)
            icons = {
                "pending":   "⏳", "active":    "🔵",
                "suspected": "🟡", "confirmed": "🔴", "discarded": "💀",
            }
            icon = icons.get(status, "❓")
            c_str = f" conf={conf:.0%}" if conf is not None else ""
            console.print(f"  [dim]  ↳ H [{h_id}] → {icon} {status}{c_str}[/]")

        elif event == "task_queued":
            t_id = data.get("id", "?")
            role = data.get("role", "?")
            console.print(f"  [blue]📮 Task [{t_id}][/] queued → role=[italic]{role}[/]")

        elif event == "drone_launched":
            t_id = data.get("task_id", "?")
            role = data.get("role", "?")
            console.print(f"  [bold blue]🚁 Drone [{t_id}][/] LAUNCHED [{role}]")

        elif event == "drone_completed":
            t_id = data.get("task_id", "?")
            console.print(f"  [green]✅ Drone [{t_id}][/] COMPLETED")

        elif event == "drone_timeout":
            t_id = data.get("task_id", "?")
            console.print(f"  [yellow]⏰ Drone [{t_id}][/] TIMEOUT")

        elif event == "drone_failed":
            t_id  = data.get("task_id", "?")
            error = data.get("error", "")[:80]
            console.print(f"  [red]❌ Drone [{t_id}][/] FAILED: {error}")

        elif event == "finding_confirmed":
            sev   = data.get("severity", "low")
            title = data.get("title", "")[:100]
            style = SEVERITY_STYLE.get(sev, "white")
            icon  = SEVERITY_ICON.get(sev, "⚪")
            console.print(
                f"\n  [{style}]{icon} FINDING [{sev.upper()}]: {title}[/]"
            )

        elif event == "tool_call":
            name = data.get("name", "?")
            args = data.get("args", "")[:80]
            console.print(f"  [dim yellow]⚙  Tool: {name}  {args}[/]")

        elif event == "arbitration_requested":
            q = data.get("question", "")[:120]
            console.print(f"\n  [bold red]⚠️  ARBITRATION REQUESTED[/]: {q}")

        elif event == "arbitration_resolved":
            approved = data.get("approved", False)
            q        = data.get("question", "")[:60]
            icon     = "✅" if approved else "❌"
            console.print(f"  [yellow]{icon} Arbitration: {'APPROVED' if approved else 'REJECTED'}[/] — {q}")

        elif event == "cerebrum_stopped":
            reason = data.get("reason", "")
            console.print(f"\n[bold yellow]🛑 CEREBRUM STOPPED: {reason}[/]")

        elif event == "cerebrum_complete":
            console.print(f"\n[bold green]🏁 ANALYSIS COMPLETE[/]")

        elif event == "terminated":
            reason = data.get("reason", "unknown")
            # 翻译终止原因为人类可读文本
            if reason == "llm_self_declared":
                label = "LLM declared all hypotheses exhausted"
                style = "bold green"
            elif reason.startswith("converged:"):
                label = f"Hypothesis tree fully converged ({reason.split(':', 1)[1]})"
                style = "bold green"
            elif reason.startswith("stagnation:"):
                label = f"No progress detected — stopping ({reason.split(':', 1)[1]})"
                style = "bold yellow"
            elif reason.startswith("budget:"):
                label = f"Budget limit reached ({reason.split(':', 1)[1]})"
                style = "bold orange1"
            else:
                label = reason
                style = "bold dim"
            console.print(f"\n[{style}]⏹  TERMINATED: {label}[/]")

        elif event == "cerebrum_error":
            error = data.get("error", "")
            console.print(f"\n[bold red]⚡ ENGINE ERROR: {error}[/]")

        # ── Sector-Based Architecture Events ──────────────────────

        elif event == "coordinator_started":
            console.print(f"\n[bold cyan]🏗️  COORDINATOR MODE — Sector-Based Analysis[/]")

        elif event == "sector_decomposition_started":
            console.print(f"  [dim]Decomposing project into security sectors...[/]")

        elif event == "sector_project_scanned":
            fc = data.get("file_count", 0)
            sz = data.get("total_size_mb", 0)
            console.print(f"  [dim]Scanned: {fc:,} files, {sz}MB[/]")

        elif event == "sector_decomposition_complete":
            sectors = data.get("sectors", [])
            console.print(f"  [green]✓ Decomposed into {len(sectors)} sectors[/]")
            for s in sectors:
                console.print(f"    [dim]P{s.get('priority', '?')} {s.get('name', '?')} ({s.get('path', '?')})[/]")

        elif event == "sector_analysis_started":
            name = data.get("sector", "?")
            path = data.get("path", "?")
            console.print(f"\n  [bold cyan]▶ Sector [{name}][/] starting — {path}")

        elif event == "sector_analysis_completed":
            name = data.get("sector", "?")
            status = data.get("status", "?")
            findings = data.get("findings", 0)
            elapsed = data.get("elapsed_seconds", 0)
            icon = "✓" if status == "done" else "✗"
            style = "green" if status == "done" else "red"
            console.print(f"  [{style}]{icon} Sector [{name}] {status} — {findings} findings, {elapsed:.0f}s[/]")

        elif event == "sector_analysis_failed":
            name = data.get("sector", "?")
            console.print(f"  [red]✗ Sector [{name}] FAILED: {data.get('error', '')[:200]}[/]")

        elif event == "cross_sector_started":
            console.print(f"\n  [bold magenta]🔗 Cross-Sector Analysis — {data.get('findings_count', 0)} findings[/]")

        elif event == "cross_sector_completed":
            chains = data.get("chains_found", 0)
            if chains > 0:
                console.print(f"  [bold magenta]🔗 Found {chains} cross-module exploit chain(s)![/]")
            else:
                console.print(f"  [dim]No cross-module chains identified.[/]")

        elif event == "coordinator_finished":
            sectors = data.get("sectors", 0)
            findings = data.get("findings", 0)
            elapsed = data.get("elapsed_seconds", 0)
            console.print(f"\n[bold green]🏁 SECTOR-BASED ANALYSIS COMPLETE[/]")
            console.print(f"  [dim]Sectors: {sectors} | Findings: {findings} | {elapsed:.0f}s[/]")


# ─────────────────────────────────────────────────────────────────────────────
#  最终报告渲染
# ─────────────────────────────────────────────────────────────────────────────

def print_final_report(blackboard: Blackboard):
    """在分析完成后打印结构化最终报告。"""
    console.print()
    console.print(Rule("[bold white] FINAL REPORT [/]", style="white"))

    stats = blackboard.stats()

    # 分类统计
    zero_day_findings = [f for f in blackboard.findings if getattr(f, "finding_type", "zero_day") == "zero_day"]
    dep_vuln_findings = [f for f in blackboard.findings if getattr(f, "finding_type", "zero_day") == "dependency_vuln"]

    # ── 统计摘要 ──
    summary = Table(box=box.SIMPLE, show_header=False, padding=(0, 2))
    summary.add_column(style="dim")
    summary.add_column(style="white")
    summary.add_row("Hypotheses total",   str(stats["hypotheses"]))
    summary.add_row("Confirmed",          f"[green]{stats['confirmed']}[/]")
    summary.add_row("Discarded",          f"[dim]{stats['discarded']}[/]")
    summary.add_row("Tasks executed",     str(stats["active_tasks"] +
                                              len([t for t in blackboard.tasks.values()
                                                   if t.status.value in ("done","failed","timeout")])))
    summary.add_row("Zero-Day Findings",  f"[bold]{len(zero_day_findings)}[/]")
    if dep_vuln_findings:
        summary.add_row("Dependency Vulns",   f"[bold blue]{len(dep_vuln_findings)}[/]")
    console.print(summary)

    # ── Zero-Day 发现详情 ──
    if zero_day_findings:
        console.print()
        console.print(Rule("[bold magenta] ZERO-DAY FINDINGS [/]", style="magenta"))
        for i, f in enumerate(zero_day_findings, 1):
            sev    = f.severity
            style  = SEVERITY_STYLE.get(sev, "white")
            icon   = SEVERITY_ICON.get(sev, "⚪")

            panel_content = (
                f"[dim]ID:[/] {f.id}\n"
                f"[dim]Severity:[/] [{style}]{sev.upper()}[/]\n"
                f"[dim]Hypothesis:[/] {f.hypothesis_id}\n\n"
                f"[bold]Description:[/]\n{f.description[:600]}\n\n"
                f"[bold]Evidence:[/]\n[dim]{f.evidence[:400]}[/]"
            )

            console.print(Panel(
                panel_content,
                title=f"[{style}]{icon} [{i}] {f.title[:80]}[/]",
                border_style=style.split()[-1],
                expand=False,
            ))
            console.print()

    # ── 依赖漏洞详情 ──
    if dep_vuln_findings:
        console.print()
        console.print(Rule("[bold blue] DEPENDENCY VULNERABILITIES (OSV) [/]", style="blue"))
        for i, f in enumerate(dep_vuln_findings, 1):
            sev    = f.severity
            style  = SEVERITY_STYLE.get(sev, "white")
            icon   = SEVERITY_ICON.get(sev, "⚪")
            pkg    = getattr(f, "package_name", "") or ""
            ver    = getattr(f, "package_version", "") or ""
            fixed  = getattr(f, "fixed_version", "") or ""
            cve    = getattr(f, "cve_id", "") or ""

            meta_lines = f"[dim]Package:[/] {pkg}@{ver}"
            if fixed:
                meta_lines += f"  |  [dim]Fixed:[/] {fixed}"
            if cve:
                meta_lines += f"  |  [dim]CVE:[/] {cve}"

            panel_content = (
                f"{meta_lines}\n"
                f"[dim]ID:[/] {f.id}\n"
                f"[dim]Severity:[/] [{style}]{sev.upper()}[/]\n\n"
                f"[bold]Description:[/]\n{f.description[:400]}\n\n"
                f"[bold]Evidence:[/]\n[dim]{f.evidence[:300]}[/]"
            )

            console.print(Panel(
                panel_content,
                title=f"[{style}]{icon} [{i}] {f.title[:80]}[/]",
                border_style=style.split()[-1],
                expand=False,
            ))
            console.print()

    if not blackboard.findings:
        console.print("[dim italic]  No confirmed findings.[/]")

    console.print(Rule(style="dim"))


# ─────────────────────────────────────────────────────────────────────────────
#  仲裁处理（支持自动批准模式）
# ─────────────────────────────────────────────────────────────────────────────

async def _arbitration_worker(blackboard: Blackboard, auto_approve: bool = True):
    """监听仲裁队列。auto_approve=True 时自动批准所有请求（无人值守模式）。"""
    while True:
        try:
            req = await asyncio.wait_for(blackboard.arbitration_queue.get(), timeout=1.0)
        except asyncio.TimeoutError:
            continue
        except asyncio.CancelledError:
            break

        question = req.get("question", "")
        context  = req.get("context", "")
        fut: asyncio.Future = req.get("future")

        if auto_approve:
            # 无人值守模式：自动批准，仅记录日志
            console.print(f"  [dim green]🤖 Auto-approved:[/] {question[:100]}")
            approved = True
        else:
            # 人工模式：等待 stdin 输入
            console.print()
            console.print(Panel(
                f"[bold white]Question:[/] {question}\n\n[dim]Context:[/] {context[:300]}",
                title="[bold red]⚠️  HUMAN ARBITRATION REQUIRED[/]",
                border_style="red",
            ))
            try:
                answer = await asyncio.to_thread(
                    input, "  Authorize? [Y/n]: "
                )
                approved = answer.strip().lower() in ("y", "yes", "")
            except (EOFError, KeyboardInterrupt):
                approved = False

            console.print(
                f"  → {'[green]AUTHORIZED[/]' if approved else '[red]REJECTED[/]'}"
            )

        if fut and not fut.done():
            fut.set_result(approved)


# ─────────────────────────────────────────────────────────────────────────────
#  报告导出
# ─────────────────────────────────────────────────────────────────────────────

def _build_report_data(blackboard: Blackboard, target: str, elapsed: float) -> dict:
    """构建结构化报告数据（JSON-ready）。"""
    stats = blackboard.stats()

    def _finding_to_dict(f):
        d = {
            "id": f.id,
            "hypothesis_id": f.hypothesis_id,
            "title": f.title,
            "severity": f.severity,
            "description": f.description,
            "evidence": f.evidence,
            "created_at": f.created_at,
            "finding_type": getattr(f, "finding_type", "zero_day"),
        }
        if getattr(f, "finding_type", "zero_day") == "dependency_vuln":
            d["cve_id"] = getattr(f, "cve_id", "")
            d["package_name"] = getattr(f, "package_name", "")
            d["package_version"] = getattr(f, "package_version", "")
            d["fixed_version"] = getattr(f, "fixed_version", "")
        return d

    zero_day_findings = [_finding_to_dict(f) for f in blackboard.findings
                         if getattr(f, "finding_type", "zero_day") == "zero_day"]
    dep_vuln_findings = [_finding_to_dict(f) for f in blackboard.findings
                         if getattr(f, "finding_type", "zero_day") == "dependency_vuln"]

    sev_key = lambda x: {"critical": 0, "high": 1, "medium": 2, "low": 3}.get(x["severity"], 4)
    zero_day_findings.sort(key=sev_key)
    dep_vuln_findings.sort(key=sev_key)

    hypotheses_list = []
    for h in blackboard.hypotheses.values():
        hypotheses_list.append({
            "id": h.id,
            "description": h.description,
            "confidence": h.confidence,
            "status": h.status.value if hasattr(h.status, 'value') else str(h.status),
            "parent_id": h.parent_id,
            "tasks_count": len(h.tasks),
            "evidence_count": len(h.evidence),
        })

    # 保留 "findings" 键以兼容外部消费者（全部 finding 合并列表）
    all_findings = zero_day_findings + dep_vuln_findings

    return {
        "engine": "HIVE-MIND INTEL ENGINE V8.0",
        "target": target,
        "timestamp": datetime.now().isoformat(),
        "elapsed_seconds": round(elapsed, 1),
        "summary": {
            "total_hypotheses": stats["hypotheses"],
            "confirmed": stats["confirmed"],
            "discarded": stats["discarded"],
            "total_findings": len(all_findings),
            "zero_day_findings": len(zero_day_findings),
            "dependency_vulns": len(dep_vuln_findings),
            "tasks_executed": len(blackboard.tasks),
        },
        "findings": all_findings,
        "zero_day_findings": zero_day_findings,
        "dependency_vulnerabilities": dep_vuln_findings,
        "hypotheses": hypotheses_list,
    }


def export_report_json(report_data: dict, output_path: Path):
    """导出 JSON 格式报告。"""
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(
        json.dumps(report_data, indent=2, ensure_ascii=False, default=str),
        encoding="utf-8",
    )


def export_report_markdown(report_data: dict, output_path: Path):
    """导出 Markdown 格式报告。"""
    output_path.parent.mkdir(parents=True, exist_ok=True)
    lines = [
        f"#  Security Analysis Report",
        f"",
        f"| Field | Value |",
        f"|-------|-------|",
        f"| Engine | {report_data['engine']} |",
        f"| Target | `{report_data['target']}` |",
        f"| Timestamp | {report_data['timestamp']} |",
        f"| Duration | {report_data['elapsed_seconds']}s |",
        f"",
        f"## Summary",
        f"",
        f"| Metric | Count |",
        f"|--------|-------|",
    ]
    s = report_data["summary"]
    lines.extend([
        f"| Hypotheses | {s['total_hypotheses']} |",
        f"| Confirmed | {s['confirmed']} |",
        f"| Discarded | {s['discarded']} |",
        f"| Zero-Day Findings | {s.get('zero_day_findings', 0)} |",
        f"| Dependency Vulns | {s.get('dependency_vulns', 0)} |",
        f"| Tasks Executed | {s['tasks_executed']} |",
        "",
    ])

    # ── Zero-Day Findings ──
    zero_day = report_data.get("zero_day_findings", [])
    if zero_day:
        lines.append("## Zero-Day Findings")
        lines.append("")
        severity_icon = {"critical": "🔴", "high": "🟠", "medium": "🟡", "low": "🔵"}
        for i, f in enumerate(zero_day, 1):
            icon = severity_icon.get(f["severity"], "⚪")
            lines.extend([
                f"### {icon} [{i}] {f['title']}",
                f"",
                f"- **Severity**: {f['severity'].upper()}",
                f"- **Hypothesis**: {f['hypothesis_id']}",
                f"- **ID**: {f['id']}",
                f"",
                f"**Description:**",
                f"",
                f"{f['description']}",
                f"",
                f"**Evidence:**",
                f"",
                f"```",
                f"{f['evidence']}",
                f"```",
                f"",
            ])
    else:
        lines.extend(["## Zero-Day Findings", "", "_No zero-day findings._", ""])

    # ── Dependency Vulnerabilities ──
    dep_vulns = report_data.get("dependency_vulnerabilities", [])
    if dep_vulns:
        lines.append("## Dependency Vulnerabilities (OSV)")
        lines.append("")
        severity_icon = {"critical": "🔴", "high": "🟠", "medium": "🟡", "low": "🔵"}
        for i, f in enumerate(dep_vulns, 1):
            icon = severity_icon.get(f["severity"], "⚪")
            meta = []
            if f.get("package_name"):
                meta.append(f"**Package**: `{f['package_name']}@{f.get('package_version', '')}`")
            if f.get("fixed_version"):
                meta.append(f"**Fixed in**: `{f['fixed_version']}`")
            if f.get("cve_id"):
                meta.append(f"**CVE**: {f['cve_id']}")
            meta_line = " | ".join(meta)

            lines.extend([
                f"### {icon} [{i}] {f['title']}",
                f"",
                f"- **Severity**: {f['severity'].upper()}",
                f"- **ID**: {f['id']}",
            ])
            if meta_line:
                lines.append(f"- {meta_line}")
            lines.extend([
                f"",
                f"**Description:**",
                f"",
                f"{f['description']}",
                f"",
                f"**Evidence:**",
                f"",
                f"```",
                f"{f['evidence']}",
                f"```",
                f"",
            ])

    lines.extend([
        "---",
        f"_Generated by {report_data['engine']}_",
    ])

    output_path.write_text("\n".join(lines), encoding="utf-8")


# ─────────────────────────────────────────────────────────────────────────────
#  Git clone 工具
# ─────────────────────────────────────────────────────────────────────────────

def _sanitize_repo_name(url: str) -> Optional[str]:
    """
    从 URL 提取并净化仓库名称，阻止路径遍历。

    Security:
    - URL 解码后验证名称
    - 拒绝包含 '..' 或路径分隔符的名称
    - 只允许安全字符（字母、数字、下划线、连字符、点号）
    - 必须以字母或数字开头
    """
    import urllib.parse

    # 提取最后一段作为仓库名
    raw_name = url.rstrip("/").split("/")[-1]
    # URL 解码
    repo_name = urllib.parse.unquote(raw_name)
    # 移除 .git 后缀
    repo_name = repo_name.replace(".git", "")
    # 移除所有非安全字符
    repo_name = re.sub(r'[^a-zA-Z0-9._-]', '', repo_name)
    # 显式阻止 '..' 和 '.' 危险模式
    if '..' in repo_name or repo_name == '.' or repo_name.startswith('.'):
        return None
    # 必须以字母或数字开头
    if not repo_name or not repo_name[0].isalnum():
        return None
    return repo_name


async def prepare_git_target(url: str, work_dir: Path) -> Optional[Path]:
    repo_name = _sanitize_repo_name(url)
    if repo_name is None:
        console.print("[bold red]❌ Invalid repository name in URL[/]")
        return None

    projects    = work_dir / "projects"
    projects.mkdir(parents=True, exist_ok=True)
    target_dir  = projects / repo_name

    console.print(f"[yellow] Cloning {url} → {target_dir}[/]")

    if not shutil.which("git"):
        console.print("[bold red]❌ git not found in PATH[/]")
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
    _, stderr = await proc.communicate()
    if proc.returncode != 0:
        console.print(f"[bold red]❌ Git error: {stderr.decode(errors='ignore').strip()}[/]")
        return None

    console.print(f"[green]✅ Ready → {target_dir}[/]")
    return target_dir


# ─────────────────────────────────────────────────────────────────────────────
#  Config 构建
# ─────────────────────────────────────────────────────────────────────────────

def build_config(api_key: str) -> Config:
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
            "provider":        "kimi",
            "model":           "kimi-for-coding",
            "max_context_size": 262144,
        }},
    )


# ─────────────────────────────────────────────────────────────────────────────
#  主异步入口
# ─────────────────────────────────────────────────────────────────────────────

async def run(target: str, work_dir: Path, root_dir: Path,
              max_workers: int, resume: bool, api_key: str,
              max_rounds: int, max_tasks: int,
              max_time: float, stagnation: int,
              auto_approve: bool = True,
              output_path: Optional[str] = None,
              no_report: bool = False):

    start_time = _time.monotonic()

    # ── 打印 Banner ──
    mode_label = "[green]UNATTENDED[/]" if auto_approve else "[yellow]INTERACTIVE[/]"
    console.print(Panel(
        f"[bold cyan]HIVE-MIND INTEL ENGINE[/]  [dim]V8.0-HEADLESS[/]\n"
        f"[dim]Autonomous · Distributed-Ready · LLM-Dominant[/]\n"
        f"Mode: {mode_label}",
        border_style="cyan",
        expand=False,
    ))

    # ── 解析目标 ──
    if re.match(r"https?://", target):
        resolved = await prepare_git_target(target, work_dir)
        if not resolved:
            return 1
        analysis_dir = resolved
    else:
        analysis_dir = Path(target).resolve()
        if not analysis_dir.exists():
            console.print(f"[bold red]❌ Target path not found: {analysis_dir}[/]")
            return 1

    console.print(f"  [dim]WORK DIR[/]  : {work_dir}")
    console.print(f"  [dim]TARGET DIR[/]: {analysis_dir}")
    console.print(f"  [dim]MAX DRONES[/]: {max_workers}")
    console.print()

    # ── 清理上次运行遗留的沙盒目录 ──
    stale_sandboxes = root_dir / "tmp" / ".drone_sandboxes"
    if stale_sandboxes.exists():
        import shutil
        shutil.rmtree(str(stale_sandboxes), ignore_errors=True)

    # ── 初始化 Blackboard ──
    blackboard = Blackboard(work_dir)
    if resume:
        blackboard.load()
        console.print("[yellow]⟳ Restored blackboard from disk[/]")

    # ── 初始化 Cerebrum ──
    session_pool = None
    if max_workers > 1:
        # 预热 Session 连接池（仅多并发模式启用）
        try:
            from engine.session_pool import DroneSessionPool
            session_pool = DroneSessionPool(
                root_dir=root_dir,
                config=build_config(api_key),
                pool_size_per_role=2,
            )
            await session_pool.initialize()
            Drone.set_pool(session_pool)   # 注入到 Drone class
        except Exception as e:
            console.print(f"[yellow]⚠️  Session Pool 初始化失败，降级到按需创建：{e}[/]")

    cerebrum = Cerebrum(
        blackboard=blackboard,
        work_dir=analysis_dir,
        root_dir=root_dir,
        config=build_config(api_key),
        max_concurrent_drones=max_workers,
        max_rounds=max_rounds,
        max_tasks=max_tasks,
        max_wall_time=max_time,
        stagnation_rounds=stagnation,
    )

    renderer = TelemetryRenderer(blackboard)

    # ── 遥测消费协程 ──
    async def consume_telemetry():
        while True:
            try:
                msg = await asyncio.wait_for(cerebrum.telemetry.get(), timeout=0.5)
                renderer.render(msg["event"], msg.get("data", {}))
            except asyncio.TimeoutError:
                continue
            except asyncio.CancelledError:
                break
            except Exception as e:
                err.print(f"[red]Telemetry error: {e}[/]")

    # ── 仲裁协程 ──
    arb_task  = asyncio.create_task(_arbitration_worker(blackboard, auto_approve=auto_approve))
    tele_task = asyncio.create_task(consume_telemetry())

    # ── 启动主脑（阻塞直到完成） ──
    exit_code = 0
    try:
        await cerebrum.launch(str(analysis_dir))
    except asyncio.CancelledError:
        # Bug Fix: KeyboardInterrupt 在 asyncio 协程内不能被 except 捕获；
        # asyncio.run() 将 Ctrl+C 转化为 CancelledError 传入协程
        console.print("\n[yellow]\u26a0\ufe0f  Interrupted by user[/]")
        await cerebrum.stop("keyboard_interrupt")
    except Exception as e:
        console.print(f"[bold red]\u274c Fatal error: {e}[/]")
        exit_code = 1
    finally:
        arb_task.cancel()
        tele_task.cancel()
        try:
            await asyncio.gather(arb_task, tele_task, return_exceptions=True)
        except Exception:
            pass
        # 排尽遗留的遥测消息，确保最后的事件被渲染
        while not cerebrum.telemetry.empty():
            try:
                msg = cerebrum.telemetry.get_nowait()
                renderer.render(msg["event"], msg.get("data", {}))
            except Exception:
                break

        # ── 关闭 Session 池 ─────────────────────────────────────
        if session_pool is not None:
            await session_pool.shutdown()
            pool_stats = session_pool.get_stats()
            total_uses = sum(v["total_uses"] for v in pool_stats.values())
            console.print(f"[dim]   [SESSION POOL] 总复用次数: {total_uses}[/]")

        # P2 FIX: 强制 GC 清理孤儿子进程传输对象。
        # kimi_agent_sdk 内部通过 asyncio.create_subprocess_exec() 创建的子进程
        # 在 session 关闭后不会被正确回收。如果等到 event loop 关闭后才被 GC 回收，
        # 其 __del__ 方法会尝试向已关闭的 loop 注册回调，产生大量
        # 'RuntimeError: Event loop is closed' 错误。
        # 解决方案：在 loop 仍然活跃时强制 GC，让 __del__ 正常执行。
        import gc
        gc.collect()
        await asyncio.sleep(0.1)  # 给 __del__ 中的 call_soon 一个执行机会
        gc.collect()

    # ── 打印最终报告（终端） ──
    print_final_report(blackboard)

    # ── FinOps 成本报告 ──
    try:
        from tools.finops_monitor import get_monitor
        fm = get_monitor()
        # Bug-19 修复：stats 必须在 try 块内通过 blackboard.stats() 获取
        stats = blackboard.stats()
        hyp_count = stats["hypotheses"]
        task_count = len(blackboard.tasks)
        # Issue-21 修复：使用更精确的 token 估算（Drone 平均 ~800 token in / ~500 token out）
        # 注：若需要更精确数据，可在 Cerebrum/Drone 的 prompt 调用处调用 finops_monitor.record_from_text()
        cerebrum_in = hyp_count * 300 + max_rounds * 500
        cerebrum_out = max_rounds * 300
        drone_in = task_count * 800
        drone_out = task_count * 500

        fm.record(role="cerebrum",
                  tokens_in=cerebrum_in,
                  tokens_out=cerebrum_out,
                  model="kimi-long-context",
                  latency_ms=0,
                  hypothesis_count=hyp_count)
        fm.record(role="drone-aggregate",
                  tokens_in=drone_in,
                  tokens_out=drone_out,
                  model="kimi-latest",
                  latency_ms=0)

        fm.print_report(console=console)

        # Session Pool 统计
        if session_pool is not None:
            fm.print_pool_stats(session_pool, console=console)
    except Exception as e:
        console.print(f"[dim][FinOps] 成本报告生成失败（非致命）: {e}[/dim]")

    # ── 导出报告文件 ──
    elapsed = _time.monotonic() - start_time
    if not no_report:
        report_data = _build_report_data(blackboard, target, elapsed)
        # 确定输出路径
        if output_path:
            out_p = Path(output_path)
        else:
            # 自动生成：./reports/<target_name>_<timestamp>.md
            target_name = Path(target).name or "analysis"
            target_name = re.sub(r'[^\w\-.]', '_', target_name)
            ts = datetime.now().strftime("%Y%m%d_%H%M%S")
            reports_dir = analysis_dir / "reports"
            reports_dir.mkdir(parents=True, exist_ok=True)
            out_p = reports_dir / f"{target_name}_{ts}.md"

        # 根据扩展名选择格式
        if out_p.suffix.lower() == ".json":
            export_report_json(report_data, out_p)
        else:
            export_report_markdown(report_data, out_p)
            # 同时导出一份 JSON（机器可读）
            json_p = out_p.with_suffix(".json")
            export_report_json(report_data, json_p)

        # ── 新增：导出利用链图（DOT + GraphML）──
        if blackboard.hypotheses:
            dot_path = out_p.with_suffix(".dot")
            gml_path = out_p.with_suffix(".graphml")
            if blackboard.export_exploit_chain_graph(dot_path):
                console.print(f"[dim]   + Graph (DOT): {dot_path}[/]")
            if blackboard.export_graphml(gml_path):
                console.print(f"[dim]   + Graph (GraphML): {gml_path}[/]")

        console.print(f"\n[bold green]📄 Report exported:[/] {out_p}")
        if out_p.suffix.lower() != ".json":
            console.print(f"[dim]   + JSON: {out_p.with_suffix('.json')}[/]")

    return exit_code


# ─────────────────────────────────────────────────────────────────────────────
#  CLI 入口
# ─────────────────────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(
        prog="kimi_hive",
        description="HIVE-MIND INTEL ENGINE — Autonomous LLM-Dominant Analysis",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  python kimi_hive.py https://github.com/owner/repo
  python kimi_hive.py /path/to/local/project
  python kimi_hive.py /path/to/project --workers 8
  python kimi_hive.py /path/to/project --resume
  python kimi_hive.py /path/to/project --work-dir ./my_workspace --workers 3
        """,
    )
    parser.add_argument(
        "target",
        help="Analysis target: GitHub URL or local directory path",
    )
    parser.add_argument(
        "--workers", "-w",
        type=int, default=3,
        help="Max concurrent drone workers (default: 3)",
    )
    parser.add_argument(
        "--work-dir", "-d",
        default="./local_workspace",
        help="Work directory for analysis artifacts (default: ./local_workspace)",
    )
    parser.add_argument(
        "--resume", "-r",
        action="store_true",
        help="Resume from previous blackboard state",
    )
    parser.add_argument(
        "--max-rounds", "-R",
        type=int, default=30,
        help="Max reasoning rounds before forced termination (default: 30)",
    )
    parser.add_argument(
        "--max-tasks", "-T",
        type=int, default=200,
        help="Max drone tasks dispatched before termination (default: 200)",
    )
    parser.add_argument(
        "--max-time", "-t",
        type=float, default=7200.0,
        help="Max wall-clock time in seconds (default: 7200 = 2h)",
    )
    parser.add_argument(
        "--stagnation", "-s",
        type=int, default=5,
        help="Stop after N consecutive rounds with no new hypotheses or findings (default: 5)",
    )
    parser.add_argument(
        "--auto-approve",
        action=argparse.BooleanOptionalAction,
        default=True,
        help="Auto-approve all arbitration requests (default: True, use --no-auto-approve for interactive)",
    )
    parser.add_argument(
        "--output", "-o",
        default=None,
        help="Report output path (default: auto-generated under ./reports/). Supports .md and .json",
    )
    parser.add_argument(
        "--no-report",
        action="store_true",
        help="Disable report export",
    )
    args = parser.parse_args()

    api_key = os.environ.get("KIMI_API_KEY")
    if not api_key:
        console.print("[bold red]❌ KIMI_API_KEY environment variable not set[/]")
        console.print("  [dim]Set it with: export KIMI_API_KEY=sk-...[/]")
        sys.exit(1)

    work_dir = Path(args.work_dir).resolve()
    work_dir.mkdir(parents=True, exist_ok=True)

    root_dir = Path(__file__).parent.resolve()

    if sys.platform == "win32":
        asyncio.set_event_loop_policy(asyncio.WindowsProactorEventLoopPolicy())

    # P2 FIX: 使用手动 event loop 生命周期管理，安装自定义异常处理器
    # 来抑制 kimi_agent_sdk 孤儿子进程传输对象在 GC 时产生的
    # 'RuntimeError: Event loop is closed' 错误洪流。
    # asyncio.run() 内部关闭 loop 后，SDK 的 BaseSubprocessTransport.__del__
    # 尝试向已关闭的 loop 注册回调，产生数百行无用错误堆栈。
    loop = asyncio.new_event_loop()
    asyncio.set_event_loop(loop)

    def _suppress_loop_closed(loop, context):
        """静默忽略 'Event loop is closed' 错误，这些来自 SDK 孤儿进程的 __del__。"""
        exc = context.get("exception")
        if isinstance(exc, RuntimeError) and "Event loop is closed" in str(exc):
            return  # 静默忽略
        # 其他异常照常打印
        loop.default_exception_handler(context)

    loop.set_exception_handler(_suppress_loop_closed)

    try:
        exit_code = loop.run_until_complete(run(
            target=args.target,
            work_dir=work_dir,
            root_dir=root_dir,
            max_workers=args.workers,
            resume=args.resume,
            api_key=api_key,
            max_rounds=args.max_rounds,
            max_tasks=args.max_tasks,
            max_time=args.max_time,
            stagnation=args.stagnation,
            auto_approve=args.auto_approve,
            output_path=args.output,
            no_report=args.no_report,
        ))
    except KeyboardInterrupt:
        exit_code = 130
    finally:
        # 在关闭 loop 前强制 GC，让孤儿传输对象的 __del__ 在 loop 活跃时执行
        import gc
        gc.collect()
        try:
            # 取消所有残留任务
            pending = asyncio.all_tasks(loop)
            for task in pending:
                task.cancel()
            if pending:
                loop.run_until_complete(asyncio.gather(*pending, return_exceptions=True))
        except Exception:
            pass

        # P2 FIX: 在关闭 loop 之前，安装 stderr 过滤器来捕获 CPython __del__
        # 直接打印的 'Exception ignored in:' 消息（这些消息绕过 asyncio 异常处理器）
        import io

        class _StderrFilter(io.TextIOWrapper):
            """过滤掉 'Event loop is closed' 相关的 __del__ 噪音。"""
            def __init__(self, wrapped):
                self._wrapped = wrapped
                self._suppressing = False

            def write(self, s):
                if "Event loop is closed" in s:
                    self._suppressing = True
                    return len(s)  # 静默吞掉
                if self._suppressing:
                    # 跟在 'Event loop is closed' 后面的堆栈行也吞掉
                    if s.startswith(("  ", "Traceback", "RuntimeError", "Exception ignored")):
                        return len(s)
                    self._suppressing = False  # 遇到非堆栈行，恢复正常输出
                return self._wrapped.write(s)

            def flush(self):
                return self._wrapped.flush()

            def __getattr__(self, name):
                return getattr(self._wrapped, name)

        sys.stderr = _StderrFilter(sys.stderr)
        loop.close()
        # 最后一次 GC：清理 loop.close() 后新产生的孤儿对象
        gc.collect()
    sys.exit(exit_code)


if __name__ == "__main__":
    main()
