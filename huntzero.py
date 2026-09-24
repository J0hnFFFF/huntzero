#!/usr/bin/env python3
"""
huntzero.py — huntzero 统一入口。

纯 CLI 扫描器，唯一子命令：

    python huntzero.py scan <target>     单次分析（替代 kimi_hive.py）
"""

import argparse
import asyncio
import logging
import os
import sys
from pathlib import Path

ROOT_DIR = Path(__file__).parent.resolve()
sys.path.insert(0, str(ROOT_DIR))


def get_api_key() -> str:
    key = os.environ.get("KIMI_API_KEY", "")
    if not key:
        print("❌ KIMI_API_KEY environment variable not set", file=sys.stderr)
        print("   Set it with: export KIMI_API_KEY=sk-...", file=sys.stderr)
        sys.exit(1)
    return key


# ─────────────────────────────────────────────────────────────────────────────
#  scan — 单次分析（委托给 kimi_hive.run，保证 100% 行为一致）
# ─────────────────────────────────────────────────────────────────────────────

async def cmd_scan(args):
    """单次分析 — 直接复用 kimi_hive.py 的 run() 函数，零行为差异。"""
    from kimi_hive import run

    api_key  = get_api_key()
    work_dir = Path(args.work_dir).resolve()
    work_dir.mkdir(parents=True, exist_ok=True)

    return await run(
        target=args.target,
        work_dir=work_dir,
        root_dir=ROOT_DIR,
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
    )


# ─────────────────────────────────────────────────────────────────────────────
#  CLI 入口
# ─────────────────────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(
        prog="huntzero",
        description="huntzero — Autonomous LLM-Dominant Security Analysis",
    )
    sub = parser.add_subparsers(dest="command", help="Available commands")

    # ── scan ──
    p_scan = sub.add_parser("scan", help="Run a single-target analysis")
    p_scan.add_argument("target", help="GitHub URL or local path")
    p_scan.add_argument("--workers", "-w", type=int, default=5)
    p_scan.add_argument("--work-dir", "-d", default="./local_workspace")
    p_scan.add_argument("--resume", "-r", action="store_true")
    p_scan.add_argument("--max-rounds", "-R", type=int, default=30)
    p_scan.add_argument("--max-tasks", "-T", type=int, default=200)
    p_scan.add_argument("--max-time", "-t", type=float, default=7200.0)
    p_scan.add_argument("--stagnation", "-s", type=int, default=3)
    p_scan.add_argument("--auto-approve", action=argparse.BooleanOptionalAction, default=True)
    p_scan.add_argument("--output", "-o", default=None)
    p_scan.add_argument("--no-report", action="store_true")

    args = parser.parse_args()

    if not args.command:
        parser.print_help()
        sys.exit(0)

    if sys.platform == "win32":
        asyncio.set_event_loop_policy(asyncio.WindowsProactorEventLoopPolicy())

    # 路由到子命令
    handlers = {
        "scan":   cmd_scan,
    }

    handler = handlers.get(args.command)
    if not handler:
        parser.print_help()
        sys.exit(1)

    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
        stream=sys.stderr,
    )

    exit_code = asyncio.run(handler(args))
    sys.exit(exit_code or 0)


if __name__ == "__main__":
    main()
