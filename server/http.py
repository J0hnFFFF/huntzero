"""
server/http.py — FastAPI REST + WebSocket（统一 Web 接口层）。

替代原有 web_server.py，使用 KimiSecCore 而非直接导入 ClusterController。
保持所有原有路由和行为不变。

Security:
- All /api/* endpoints require API key authentication
- API key is passed via X-API-Key header
- Configure via KIMISEC_API_KEY environment variable
"""

import json
import logging
import os
from typing import Optional

from fastapi import FastAPI, WebSocket, WebSocketDisconnect, HTTPException, Query, Security, Depends, status
from fastapi.responses import HTMLResponse
from fastapi.security import APIKeyHeader
from fastapi.staticfiles import StaticFiles
from pydantic import BaseModel, Field

from .core import KimiSecCore, EVENTS_CHANNEL, QUEUE_KEY, ROOT_DIR

logger = logging.getLogger("kimisec.http")

# ── API Key Authentication ────────────────────────────────────────────────────

# API key from environment variable
API_KEY_NAME = "X-API-Key"
api_key_header = APIKeyHeader(name=API_KEY_NAME, auto_error=False)


async def verify_api_key(api_key: Optional[str] = Security(api_key_header)) -> str:
    """
    Verify API key from X-API-Key header.

    Security:
    - Rejects requests without API key
    - Validates API key against environment variable
    - Uses constant-time comparison to prevent timing attacks
    """
    expected_key = os.environ.get("KIMISEC_API_KEY")

    # Check if API key is configured
    if not expected_key:
        logger.error("KIMISEC_API_KEY not configured")
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail="Server configuration error: API key not set"
        )

    # Check if API key is provided
    if not api_key:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Missing API key. Provide via X-API-Key header."
        )

    # Constant-time comparison to prevent timing attacks
    if not (api_key == expected_key):
        logger.warning("Invalid API key attempt")
        raise HTTPException(
            status_code=status.HTTP_403_FORBIDDEN,
            detail="Invalid API key"
        )

    return api_key


# Dependency for optional auth (for endpoints that might be public in future)
async def verify_api_key_optional(api_key: Optional[str] = Security(api_key_header)) -> Optional[str]:
    """Optional API key verification (currently unused, for future extensibility)."""
    if not api_key:
        return None
    try:
        return await verify_api_key(api_key)
    except HTTPException:
        return None


# ─── Pydantic 模型（与 web_server.py 完全一致）──────────────────────────────

class TargetItem(BaseModel):
    target: str = Field(..., description="GitHub URL 或本地路径")
    max_workers: int = Field(5, ge=1, le=32)
    max_rounds: int = Field(30, ge=1, le=200)
    max_tasks: int = Field(200, ge=1, le=2000)
    max_wall_time: float = Field(7200.0, ge=60.0)
    stagnation_rounds: int = Field(3, ge=1, le=20)

class SubmitRequest(BaseModel):
    targets: list[TargetItem] = Field(..., min_length=1)


# ─── App 工厂（接收 KimiSecCore 实例）────────────────────────────────────────

def create_app(core: KimiSecCore) -> FastAPI:
    app = FastAPI(
        title="kimiSec Console",
        version="8.0.0",
        description="Unified kimiSec management console.",
    )

    # ── 静态文件托管（Vue 3 SPA）──
    dist_dir = ROOT_DIR / "web_ui" / "dist"

    if dist_dir.exists():
        app.mount("/assets", StaticFiles(directory=str(dist_dir / "assets")), name="assets")
        app.mount("/ui", StaticFiles(directory=str(dist_dir), html=True), name="ui")

    @app.get("/", response_class=HTMLResponse, include_in_schema=False)
    async def root():
        index = dist_dir / "index.html"
        if index.exists():
            return HTMLResponse(content=index.read_text(encoding="utf-8"))
        return HTMLResponse(content="<h1>kimiSec</h1><p>Web UI not built. Run <code>cd web_ui_v2 && npm run build</code></p>")

    # ── REST API（与 web_server.py 路由完全一致）──

    @app.post("/api/jobs", summary="提交分析目标到集群队列", dependencies=[Depends(verify_api_key)])
    async def submit_jobs(body: SubmitRequest):
        result = core.submit_targets([t.model_dump() for t in body.targets])
        return {"ok": True, "message": result}

    @app.get("/api/cluster", summary="获取集群实时状态看板", dependencies=[Depends(verify_api_key)])
    async def get_cluster():
        return core.get_cluster_dashboard()

    @app.get("/api/findings", summary="查询全局漏洞库", dependencies=[Depends(verify_api_key)])
    async def get_findings(
        severity: Optional[str] = Query(None, enum=["critical", "high", "medium", "low"]),
        keyword:  Optional[str] = Query(None),
        limit:    int = Query(50, ge=1, le=500),
    ):
        findings = core.get_findings(severity=severity, keyword=keyword, limit=limit)
        return {"total": len(findings), "findings": findings}

    @app.get("/api/insights", summary="跨项目漏洞模式分析", dependencies=[Depends(verify_api_key)])
    async def get_insights():
        return {"report": core.get_cross_project_insights()}

    @app.delete("/api/jobs/{job_id}", summary="取消待执行任务", dependencies=[Depends(verify_api_key)])
    async def cancel_job(job_id: str):
        result = core.cancel_job(job_id)
        return {"ok": True, "message": result}

    @app.get("/api/queue", summary="获取队列中所有待执行任务", dependencies=[Depends(verify_api_key)])
    async def get_queue():
        if not core.redis_url:
            raise HTTPException(status_code=400, detail="Redis not configured")
        try:
            import redis.asyncio as aioredis
            r = aioredis.from_url(core.redis_url, decode_responses=True)
            items = await r.lrange(QUEUE_KEY, 0, -1)
            await r.aclose()
            parsed = []
            for item in items:
                try:
                    parsed.append(json.loads(item))
                except Exception:
                    parsed.append({"raw": item})
            return {"queue_depth": len(parsed), "jobs": parsed}
        except Exception as e:
            raise HTTPException(status_code=503, detail=f"Redis error: {e}")

    @app.get("/api/jobs", summary="列出所有已知 Job", dependencies=[Depends(verify_api_key)])
    async def list_jobs():
        if not core.redis_url:
            raise HTTPException(status_code=400, detail="Redis not configured")
        try:
            import redis.asyncio as aioredis
            r = aioredis.from_url(core.redis_url, decode_responses=True)
            keys = []
            async for key in r.scan_iter("kimisec:node:*:snapshot"):
                keys.append(key)
            await r.aclose()
            job_ids = [k.split(":")[2] for k in keys if len(k.split(":")) >= 4]
            return {"total": len(job_ids), "job_ids": sorted(job_ids)}
        except Exception as e:
            raise HTTPException(status_code=503, detail=f"Redis error: {e}")

    @app.get("/api/jobs/{job_id}/snapshot", summary="获取 Job 完整快照", dependencies=[Depends(verify_api_key)])
    async def get_job_snapshot(job_id: str):
        if not core.redis_url:
            raise HTTPException(status_code=400, detail="Redis not configured")
        try:
            import redis.asyncio as aioredis
            r = aioredis.from_url(core.redis_url, decode_responses=True)
            raw = await r.get(f"kimisec:node:{job_id}:snapshot")
            await r.aclose()
            if raw is None:
                raise HTTPException(status_code=404, detail=f"Job {job_id} snapshot not found")
            return json.loads(raw)
        except HTTPException:
            raise
        except Exception as e:
            raise HTTPException(status_code=503, detail=f"Redis error: {e}")

    @app.get("/api/jobs/{job_id}/audit", summary="获取 Job 审计报告", dependencies=[Depends(verify_api_key)])
    async def get_job_audit(job_id: str):
        if not core.redis_url:
            raise HTTPException(status_code=400, detail="Redis not configured")
        try:
            import redis.asyncio as aioredis
            r = aioredis.from_url(core.redis_url, decode_responses=True)
            raw = await r.get(f"kimisec:node:{job_id}:audit")
            await r.aclose()
            if raw is None:
                raise HTTPException(status_code=404, detail=f"Job {job_id} audit not found")
            return {"job_id": job_id, "report": raw}
        except HTTPException:
            raise
        except Exception as e:
            raise HTTPException(status_code=503, detail=f"Redis error: {e}")

    # ── Workspace 管理 API（文件系统操作，不依赖 Redis）──

    workspace_dir = core.work_dir
    projects_dir = workspace_dir / "projects"

    # ── 路径遍历保护 ────────────────────────────────────────────────────────
    import re as _re

    def _validate_project_path(name: str) -> Path:
        """
        验证项目名称并返回安全的解析后路径。

        Security:
        - 拒绝包含路径遍历字符的名称
        - 解析路径并验证其在 projects_dir 内
        - 显式阻止 '..' 和其他危险模式
        """
        # 严格验证：只允许简单名称（必须以字母或数字开头）
        if not _re.match(r'^\w[\w.-]*$', name):
            raise HTTPException(status_code=400, detail="Invalid project name")
        # 显式阻止 '..' 和 '.' 危险模式
        if '..' in name or name == '.' or name.startswith('.'):
            raise HTTPException(status_code=400, detail="Invalid project name")

        # 构造并解析路径
        proj = (projects_dir / name).resolve()
        projects_resolved = projects_dir.resolve()

        # 验证解析后的路径在 projects_dir 内
        try:
            proj.relative_to(projects_resolved)
        except ValueError:
            raise HTTPException(status_code=403, detail="Access denied")

        return proj

    def _validate_filename(filename: str) -> str:
        """
        验证文件名并阻止路径遍历。

        Security:
        - 拒绝包含路径遍历字符的文件名
        - 阻止绝对路径
        - 显式阻止 '..' 和 '.' 危险模式
        - 只允许安全的文件名字符
        """
        # 拒绝路径遍历
        if '..' in filename or '/' in filename or '\\' in filename:
            raise HTTPException(status_code=400, detail="Invalid filename")
        # 拒绝绝对路径
        if Path(filename).is_absolute():
            raise HTTPException(status_code=400, detail="Invalid filename")
        # 显式阻止 '..' 和 '.' 危险模式
        if '..' in filename or filename == '.' or filename.startswith('.'):
            raise HTTPException(status_code=400, detail="Invalid filename")
        # 验证文件名格式（只允许字母、数字、下划线、连字符、点号）
        if not _re.match(r'^[\w.-]+$', filename):
            raise HTTPException(status_code=400, detail="Invalid filename")
        return filename

    @app.get("/api/workspace/projects", summary="列出所有已扫描项目", dependencies=[Depends(verify_api_key)])
    async def list_projects():
        if not projects_dir.exists():
            return {"total": 0, "projects": []}
        projects = []
        for p in sorted(projects_dir.iterdir()):
            if not p.is_dir() or p.name.startswith("."):
                continue
            reports_dir = p / "reports"
            report_files = list(reports_dir.glob("*.md")) if reports_dir.exists() else []
            json_reports = list(reports_dir.glob("*.json")) if reports_dir.exists() else []
            # Blackboard
            bb = p / ".blackboard.json"
            bb_size = bb.stat().st_size if bb.exists() else 0
            # Disk usage estimation (top-level files only for speed)
            try:
                total_size = sum(
                    f.stat().st_size for f in p.rglob("*") if f.is_file()
                )
            except Exception:
                total_size = 0
            # Last modified
            try:
                last_mod = max(
                    (f.stat().st_mtime for f in p.rglob("*") if f.is_file()),
                    default=0,
                )
            except Exception:
                last_mod = 0

            projects.append({
                "name": p.name,
                "reports_count": len(report_files),
                "json_reports_count": len(json_reports),
                "has_blackboard": bb.exists(),
                "blackboard_size": bb_size,
                "disk_size_mb": round(total_size / 1024 / 1024, 2),
                "last_modified": last_mod,
            })
        return {"total": len(projects), "projects": projects}

    @app.get("/api/workspace/projects/{name}/reports", summary="列出项目报告", dependencies=[Depends(verify_api_key)])
    async def list_project_reports(name: str):
        proj = _validate_project_path(name)
        if not proj.exists():
            raise HTTPException(status_code=404, detail=f"Project '{name}' not found")
        reports_dir = proj / "reports"
        files = []
        if reports_dir.exists():
            for f in sorted(reports_dir.iterdir(), key=lambda x: x.stat().st_mtime, reverse=True):
                if f.is_file() and f.suffix in (".md", ".json"):
                    files.append({
                        "filename": f.name,
                        "type": f.suffix.lstrip("."),
                        "size": f.stat().st_size,
                        "modified": f.stat().st_mtime,
                    })
        # Also check for blackboard and audit_notes
        bb = proj / ".blackboard.json"
        audit = proj / ".audit_notes.md"
        meta = {
            "has_blackboard": bb.exists(),
            "has_audit_notes": audit.exists(),
        }
        return {"project": name, "reports": files, "meta": meta}

    @app.get("/api/workspace/projects/{name}/reports/{filename}", summary="读取报告内容", dependencies=[Depends(verify_api_key)])
    async def read_project_report(name: str, filename: str):
        # 验证项目名
        proj = _validate_project_path(name)
        # 验证文件名
        safe_filename = _validate_filename(filename)
        # 构造安全路径
        fpath = (proj / "reports" / safe_filename).resolve()
        reports_dir = (proj / "reports").resolve()
        # 验证文件路径在 reports 目录内
        try:
            fpath.relative_to(reports_dir)
        except ValueError:
            raise HTTPException(status_code=403, detail="Access denied")
        if not fpath.exists() or not fpath.is_file():
            raise HTTPException(status_code=404, detail=f"Report not found: {filename}")
        content = fpath.read_text(encoding="utf-8", errors="replace")
        return {
            "project": name,
            "filename": filename,
            "type": fpath.suffix.lstrip("."),
            "size": fpath.stat().st_size,
            "content": content,
        }

    @app.get("/api/workspace/projects/{name}/blackboard", summary="读取项目 Blackboard", dependencies=[Depends(verify_api_key)])
    async def read_project_blackboard(name: str):
        proj = _validate_project_path(name)
        bb = proj / ".blackboard.json"
        if not bb.exists():
            raise HTTPException(status_code=404, detail="Blackboard not found")
        try:
            data = json.loads(bb.read_text(encoding="utf-8"))
            return data
        except Exception as e:
            raise HTTPException(status_code=500, detail=f"Parse error: {e}")

    @app.delete("/api/workspace/projects/{name}", summary="删除项目（清理磁盘）", dependencies=[Depends(verify_api_key)])
    async def delete_project(name: str):
        proj = _validate_project_path(name)
        if not proj.exists():
            raise HTTPException(status_code=404, detail=f"Project '{name}' not found")
        import shutil as _shutil
        try:
            _shutil.rmtree(str(proj))
            return {"ok": True, "message": f"Deleted project '{name}'"}
        except Exception as e:
            raise HTTPException(status_code=500, detail=f"Delete failed: {e}")

    # ── WebSocket 实时事件流 ──

    @app.websocket("/ws/events")
    async def ws_events(websocket: WebSocket):
        # Authenticate WebSocket connection
        # API key can be passed as query parameter: ?api_key=xxx
        api_key = websocket.query_params.get("api_key") or websocket.headers.get("X-API-Key")

        # Verify API key
        expected_key = os.environ.get("KIMISEC_API_KEY")
        if not expected_key:
            await websocket.close(code=4000, reason="Server configuration error")
            return

        if not api_key or not (api_key == expected_key):
            await websocket.close(code=4001, reason="Unauthorized: Invalid or missing API key")
            return

        if not core.redis_url:
            await websocket.close(code=4000, reason="Redis not configured")
            return

        await websocket.accept()
        import redis.asyncio as aioredis
        r = aioredis.from_url(core.redis_url, decode_responses=True)
        pubsub = r.pubsub()

        try:
            await pubsub.subscribe(EVENTS_CHANNEL)
            await websocket.send_text(json.dumps({
                "type": "connected",
                "data": {"channel": EVENTS_CHANNEL},
            }))

            async for message in pubsub.listen():
                if message["type"] == "message":
                    data = message["data"]
                    try:
                        parsed = json.loads(data)
                        payload = json.dumps(parsed, ensure_ascii=False)
                    except Exception:
                        payload = json.dumps({"type": "raw", "data": data}, ensure_ascii=False)
                    await websocket.send_text(payload)

        except WebSocketDisconnect:
            pass
        except Exception as e:
            logger.error("WebSocket error: %s", e)
        finally:
            await pubsub.unsubscribe(EVENTS_CHANNEL)
            await r.aclose()

    return app
