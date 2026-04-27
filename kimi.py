#!/usr/bin/env python3
"""
kimi.py — kimiSec 统一入口。

合并原有 5 个入口文件为 4 个子命令：

    python kimi.py scan <target>         单次分析（替代 kimi_hive.py）
    python kimi.py serve [--redis ...]   启动服务（MCP + HTTP，替代 kimi_mcp_server / kimi_cluster_mcp / web_server）
    python kimi.py worker --redis ...    启动 Worker 节点（替代 kimi_worker.py）
    python kimi.py feed --redis ...      启动自动目标投喂（新功能）

所有原有文件保持不变，作为向后兼容入口。
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
#  serve — 统一服务（MCP + HTTP）
# ─────────────────────────────────────────────────────────────────────────────

async def cmd_serve(args):
    """启动统一服务：MCP（stdio/TCP） + HTTP（REST + WebSocket）。"""
    from server.core import KimiSecCore
    from server.mcp import stdio_loop, tcp_server

    redis_url = args.redis or os.environ.get("KIMI_REDIS_URL")
    api_key   = get_api_key()

    core = KimiSecCore(
        redis_url=redis_url,
        work_dir=args.work_dir,
    )

    mode_label = "cluster" if redis_url else "local"
    print(f"[kimisec] Mode: {mode_label}", file=sys.stderr, flush=True)

    tasks = []

    # MCP 服务
    if args.mcp_port:
        tasks.append(tcp_server(core, api_key, host=args.host, port=args.mcp_port))
        print(f"[kimisec] MCP TCP → {args.host}:{args.mcp_port}", file=sys.stderr, flush=True)

    # HTTP 服务
    if args.http_port:
        from server.http import create_app
        import uvicorn

        app = create_app(core)

        config = uvicorn.Config(
            app,
            host=args.host,
            port=args.http_port,
            log_level="info",
        )
        server = uvicorn.Server(config)
        tasks.append(server.serve())
        print(f"[kimisec] HTTP → http://{args.host}:{args.http_port}", file=sys.stderr, flush=True)

    # 如果没有指定 TCP MCP 端口，默认走 stdio
    if not args.mcp_port and not args.http_port:
        print("[kimisec] Starting MCP stdio server...", file=sys.stderr, flush=True)
        await stdio_loop(core, api_key)
        return 0

    await asyncio.gather(*tasks)
    return 0


# ─────────────────────────────────────────────────────────────────────────────
#  worker — Redis BLPOP Worker
# ─────────────────────────────────────────────────────────────────────────────

async def cmd_worker(args):
    """启动 Worker 节点，从 Redis 队列持续消费目标。"""
    from server.worker import worker_loop

    redis_url = args.redis or os.environ.get("KIMI_REDIS_URL")
    if not redis_url:
        print("❌ --redis or KIMI_REDIS_URL required for worker mode.", file=sys.stderr)
        return 1

    api_key   = get_api_key()
    work_root = Path(args.work_dir).resolve()
    work_root.mkdir(parents=True, exist_ok=True)

    await worker_loop(redis_url, api_key, work_root)
    return 0


# ─────────────────────────────────────────────────────────────────────────────
#  feed — 自动目标投喂（后续实现）
# ─────────────────────────────────────────────────────────────────────────────

async def cmd_feed(args):
    """自动从 GitHub 获取目标并投入队列。"""
    redis_url = args.redis or os.environ.get("KIMI_REDIS_URL")
    if not redis_url:
        print("❌ --redis or KIMI_REDIS_URL required for feed mode.", file=sys.stderr)
        return 1

    from server.core import KimiSecCore
    core = KimiSecCore(redis_url=redis_url)

    print(f"[kimisec] Feeder started. Redis: {redis_url}", flush=True)
    print("[kimisec] Feed sources not yet configured. Use submit_targets API to add targets.", flush=True)
    print("[kimisec] TODO: Implement GitHub Trending / Search / Topic auto-feed.", flush=True)

    # 占位：保持进程运行，等后续实现
    try:
        while True:
            await asyncio.sleep(60)
    except (KeyboardInterrupt, asyncio.CancelledError):
        pass

    return 0


async def cmd_binrev(args):
    """将参数转发给 binrev 子 CLI。"""
    from binrev.cli import invoke as binrev_invoke

    return await binrev_invoke(args.binrev_args)


# ─────────────────────────────────────────────────────────────────────────────
#  CLI 入口
# ─────────────────────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(
        prog="kimi",
        description="kimiSec — Autonomous LLM-Dominant Security Analysis",
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

    # ── serve ──
    p_serve = sub.add_parser("serve", help="Start unified server (MCP + HTTP)")
    p_serve.add_argument("--redis", default=None, help="Redis URL (enables cluster mode)")
    p_serve.add_argument("--work-dir", "-d", default="./local_workspace")
    p_serve.add_argument("--host", default="0.0.0.0")
    p_serve.add_argument("--mcp-port", type=int, default=None, help="MCP TCP port (default: stdio)")
    p_serve.add_argument("--http-port", type=int, default=None, help="HTTP port (default: disabled)")

    # ── worker ──
    p_worker = sub.add_parser("worker", help="Start Redis BLPOP worker node")
    p_worker.add_argument("--redis", "-r", default=None, help="Redis URL")
    p_worker.add_argument("--work-dir", "-d", default="./local_workspace/workers")

    # ── feed ──
    p_feed = sub.add_parser("feed", help="Start auto target feeder")
    p_feed.add_argument("--redis", "-r", default=None, help="Redis URL")

    # ── binrev ──
    p_binrev = sub.add_parser("binrev", help="Run BinRev binary-analysis commands")
    p_binrev.add_argument(
        "binrev_args",
        nargs=argparse.REMAINDER,
        help="Arguments forwarded to the BinRev CLI, e.g. analyze sample.exe --resume",
    )

    args = parser.parse_args()

    if not args.command:
        parser.print_help()
        sys.exit(0)

    if sys.platform == "win32":
        asyncio.set_event_loop_policy(asyncio.WindowsProactorEventLoopPolicy())

    # 路由到子命令
    handlers = {
        "scan":   cmd_scan,
        "serve":  cmd_serve,
        "worker": cmd_worker,
        "feed":   cmd_feed,
        "binrev": cmd_binrev,
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
