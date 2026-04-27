"""
server/mcp.py — 统一 MCP 协议层。

合并原有 kimi_mcp_server.py + kimi_cluster_mcp.py 的 MCP 工具定义和 JSON-RPC 处理。
一份代码同时支持 local 和 cluster 模式的全部工具。
支持 stdio 和 TCP 两种传输。
"""

import asyncio
import json
import logging
import sys
from typing import Optional

from .core import KimiSecCore

logger = logging.getLogger("kimisec.mcp")

# ─────────────────────────────────────────────────────────────────────────────
#  工具定义（合并 local + cluster，全部暴露）
# ─────────────────────────────────────────────────────────────────────────────

TOOLS = [
    # ── 原 kimi_mcp_server.py 工具 ──
    {
        "name": "start_analysis",
        "description": (
            "Launch a full autonomous Hive-Mind security analysis on a target. "
            "The target can be a GitHub repository URL or a local directory path. "
            "Analysis runs in the background; use get_status() to monitor progress."
        ),
        "inputSchema": {
            "type": "object",
            "properties": {
                "target":            {"type": "string", "description": "GitHub URL or local path"},
                "max_workers":       {"type": "integer", "default": 5},
                "max_rounds":        {"type": "integer", "default": 30},
                "max_tasks":         {"type": "integer", "default": 200},
                "max_wall_time":     {"type": "number",  "default": 7200.0},
                "stagnation_rounds": {"type": "integer", "default": 3},
            },
            "required": ["target"],
        },
    },
    {
        "name": "get_status",
        "description": "Get current analysis status (local) or cluster dashboard (cluster mode).",
        "inputSchema": {"type": "object", "properties": {}},
    },
    {
        "name": "get_findings",
        "description": "Retrieve confirmed security findings. Supports severity/keyword filters.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "severity": {"type": "string", "enum": ["critical", "high", "medium", "low"]},
                "keyword":  {"type": "string"},
                "limit":    {"type": "integer", "default": 50},
            },
        },
    },
    {
        "name": "stop_analysis",
        "description": "Stop the currently running analysis (local mode).",
        "inputSchema": {"type": "object", "properties": {}},
    },
    # ── 原 kimi_cluster_mcp.py 工具 ──
    {
        "name": "submit_bulk_targets",
        "description": (
            "Submit a batch of targets to the distributed cluster queue. "
            "Each target will be picked up by an idle Worker node."
        ),
        "inputSchema": {
            "type": "object",
            "properties": {
                "targets": {
                    "type": "array",
                    "items": {
                        "oneOf": [
                            {"type": "string"},
                            {
                                "type": "object",
                                "properties": {
                                    "target":            {"type": "string"},
                                    "max_workers":       {"type": "integer", "default": 5},
                                    "max_rounds":        {"type": "integer", "default": 30},
                                    "max_tasks":         {"type": "integer", "default": 200},
                                    "max_wall_time":     {"type": "number",  "default": 7200.0},
                                    "stagnation_rounds": {"type": "integer", "default": 3},
                                },
                                "required": ["target"],
                            },
                        ]
                    },
                },
            },
            "required": ["targets"],
        },
    },
    {
        "name": "get_cluster_dashboard",
        "description": "Get real-time status of all Worker nodes in the cluster.",
        "inputSchema": {"type": "object", "properties": {}},
    },
    {
        "name": "query_global_findings",
        "description": "Search across ALL findings from the entire cluster.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "severity": {"type": "string", "enum": ["critical", "high", "medium", "low"]},
                "keyword":  {"type": "string"},
                "limit":    {"type": "integer", "default": 50},
            },
        },
    },
    {
        "name": "get_cross_project_insights",
        "description": "Analyze recurring vulnerability patterns across all scanned projects.",
        "inputSchema": {"type": "object", "properties": {}},
    },
    {
        "name": "cancel_job",
        "description": "Remove a job from the pending queue before a worker picks it up.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "job_id": {"type": "string"},
            },
            "required": ["job_id"],
        },
    },
]

# ─────────────────────────────────────────────────────────────────────────────
#  JSON-RPC 辅助
# ─────────────────────────────────────────────────────────────────────────────

def _resp(req_id, result):
    return {"jsonrpc": "2.0", "id": req_id, "result": result}

def _err(req_id, code, msg):
    return {"jsonrpc": "2.0", "id": req_id, "error": {"code": code, "message": msg}}

def _text(t):
    return {"type": "text", "text": t}

def _format_findings(findings: list) -> str:
    if not findings:
        return "No findings match the given criteria."
    lines = [f"## {len(findings)} Finding(s)\n"]
    for f in findings:
        icon = {"critical": "🔴", "high": "🟠", "medium": "🟡", "low": "🔵"}.get(
            f.get("severity", ""), "⚪")
        lines.append(f"### {icon} [{f.get('severity','?').upper()}] {f.get('title','')}")
        lines.append(f"**Description**: {f.get('description','')[:400]}")
        lines.append(f"**Evidence**: `{f.get('evidence','')[:200]}`\n")
    return "\n".join(lines)

# ─────────────────────────────────────────────────────────────────────────────
#  工具分发
# ─────────────────────────────────────────────────────────────────────────────

async def _dispatch(core: KimiSecCore, api_key: str, name: str, args: dict) -> str:
    # ── Local 工具 ──
    if name == "start_analysis":
        kw = {k: args[k] for k in
              ("max_workers","max_rounds","max_tasks","max_wall_time","stagnation_rounds")
              if k in args}
        return await core.start_local(args.get("target", ""), api_key, **kw)

    if name == "get_status":
        return json.dumps(core.get_status(), ensure_ascii=False, indent=2)

    if name == "get_findings":
        return _format_findings(core.get_findings(
            severity=args.get("severity"), keyword=args.get("keyword"),
            limit=int(args.get("limit", 50)),
        ))

    if name == "stop_analysis":
        return await core.stop_local()

    # ── Cluster 工具 ──
    if name == "submit_bulk_targets":
        return core.submit_targets(args.get("targets", []))

    if name == "get_cluster_dashboard":
        return json.dumps(core.get_cluster_dashboard(), ensure_ascii=False, indent=2)

    if name == "query_global_findings":
        return _format_findings(core.query_global_findings(
            severity=args.get("severity"), keyword=args.get("keyword"),
            limit=int(args.get("limit", 50)),
        ))

    if name == "get_cross_project_insights":
        return core.get_cross_project_insights()

    if name == "cancel_job":
        return core.cancel_job(args.get("job_id", ""))

    raise ValueError(f"Unknown tool: {name}")

# ─────────────────────────────────────────────────────────────────────────────
#  请求处理
# ─────────────────────────────────────────────────────────────────────────────

async def handle(core: KimiSecCore, api_key: str, msg: dict) -> Optional[dict]:
    method = msg.get("method", "")
    req_id = msg.get("id")
    params = msg.get("params", {})

    if method == "initialize":
        return _resp(req_id, {
            "protocolVersion": "2024-11-05",
            "capabilities": {"tools": {}},
            "serverInfo": {"name": "kimisec", "version": "8.0.0"},
        })
    if method == "notifications/initialized":
        return None
    if method == "tools/list":
        return _resp(req_id, {"tools": TOOLS})
    if method == "ping":
        return _resp(req_id, {})

    if method == "tools/call":
        name = params.get("name", "")
        args = params.get("arguments", {})
        try:
            text = await _dispatch(core, api_key, name, args)
            return _resp(req_id, {"content": [_text(text)], "isError": False})
        except Exception as e:
            logger.exception("Tool error")
            return _resp(req_id, {"content": [_text(f"❌ Error: {e}")], "isError": True})

    if req_id is not None:
        return _err(req_id, -32601, f"Method not found: {method}")
    return None

# ─────────────────────────────────────────────────────────────────────────────
#  传输层：stdio
# ─────────────────────────────────────────────────────────────────────────────

async def stdio_loop(core: KimiSecCore, api_key: str):
    loop = asyncio.get_event_loop()
    reader = asyncio.StreamReader()
    await loop.connect_read_pipe(lambda: asyncio.StreamReaderProtocol(reader), sys.stdin)
    wt, _ = await loop.connect_write_pipe(asyncio.BaseProtocol, sys.stdout.buffer)

    def send(obj):
        wt.write((json.dumps(obj, ensure_ascii=False) + "\n").encode())

    logger.info("MCP server started (stdio)")

    while True:
        try:
            line = await reader.readline()
        except Exception:
            break
        if not line:
            break
        line = line.strip()
        if not line:
            continue
        try:
            m = json.loads(line)
        except json.JSONDecodeError:
            continue
        try:
            resp = await handle(core, api_key, m)
            if resp is not None:
                send(resp)
        except Exception as e:
            logger.exception(e)
            if m.get("id") is not None:
                send(_err(m["id"], -32603, str(e)))

# ─────────────────────────────────────────────────────────────────────────────
#  传输层：TCP
# ─────────────────────────────────────────────────────────────────────────────

async def tcp_server(core: KimiSecCore, api_key: str, host: str = "0.0.0.0", port: int = 9999):
    async def _session(reader, writer):
        peer = writer.get_extra_info("peername", "<unknown>")
        logger.info("MCP client connected: %s", peer)
        def send(obj):
            writer.write((json.dumps(obj, ensure_ascii=False) + "\n").encode())
        try:
            while True:
                try:
                    line = await reader.readline()
                except Exception:
                    break
                if not line:
                    break
                line = line.strip()
                if not line:
                    continue
                try:
                    m = json.loads(line)
                except json.JSONDecodeError:
                    continue
                try:
                    resp = await handle(core, api_key, m)
                    if resp is not None:
                        send(resp)
                        await writer.drain()
                except Exception as e:
                    logger.exception(e)
                    if m.get("id") is not None:
                        send(_err(m["id"], -32603, str(e)))
                        await writer.drain()
        finally:
            logger.info("MCP client disconnected: %s", peer)
            writer.close()

    server = await asyncio.start_server(_session, host, port)
    addr = server.sockets[0].getsockname()
    logger.info("MCP TCP server on %s:%s", addr[0], addr[1])
    print(f"[kimisec] MCP server listening on {addr[0]}:{addr[1]}", flush=True)
    async with server:
        await server.serve_forever()
