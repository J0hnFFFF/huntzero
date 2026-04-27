"""
sync_intel.py — 内网漏洞情报同步推送工具。

将 Blackboard 中确认的 Finding 推送至外网官网 (website_server.py)。
使用 HMAC-SHA256 签名认证，确保数据传输安全。

── 架构 ──
  内网 kimi.py (Finding)  ──HTTPS POST──>  外网 website_server.py (/api/intel)
                                                     ↓
                                              data/intel.json
                                                     ↓
                                           intelligence.html (前端展示)

── 使用方式 ──

  # 1. 推送单条（手动）
  python sync_intel.py push \
      --url https://lieling.xyz/api/intel \
      --secret "your-shared-secret" \
      --component "fastjson" --version "1.2.83" \
      --title "反序列化 RCE" \
      --severity critical --cvss 9.8 \
      --description "通过 autoType 绕过实现远程代码执行" \
      --visibility locked

  # 2. 从 blackboard.json 同步所有 findings
  python sync_intel.py sync \
      --url https://lieling.xyz/api/intel \
      --secret "your-shared-secret" \
      --blackboard ./local_workspace/.blackboard.json

  # 3. 列出外网当前的情报（调试用）
  python sync_intel.py list --url https://lieling.xyz/api/intel
"""

import argparse
import hashlib
import hmac
import json
import sys
import time
from pathlib import Path

try:
    import requests
except ImportError:
    print("需要安装 requests: pip install requests")
    sys.exit(1)


def sign(payload: bytes, secret: str) -> str:
    """生成 HMAC-SHA256 签名"""
    return hmac.new(secret.encode(), payload, hashlib.sha256).hexdigest()


def push_items(api_url: str, secret: str, items: list) -> dict:
    """
    将漏洞情报列表推送到外网 API。

    Args:
        api_url: 外网 API 地址，如 https://lieling.xyz/api/intel
        secret:  HMAC 签名密钥（必须与外网 INTEL_SECRET 环境变量一致）
        items:   漏洞情报列表

    Returns:
        API 响应 dict
    """
    payload = json.dumps({"items": items}, ensure_ascii=False).encode("utf-8")
    signature = sign(payload, secret)

    resp = requests.post(
        api_url,
        data=payload,
        headers={
            "Content-Type": "application/json",
            "X-Signature": signature,
        },
        timeout=30,
    )
    resp.raise_for_status()
    return resp.json()


def finding_to_intel(finding: dict, visibility: str = "locked") -> dict:
    """
    将 Blackboard Finding 格式转为 intel 推送格式。

    Finding 字段:
      id, hypothesis_id, title, description, severity, evidence, created_at

    Intel 字段:
      id, component, version, title, description, severity, cvss,
      status, visibility, discovered_at, evidence
    """
    # 从 hypothesis_id 推断组件名（格式通常为 "hyp-组件-xxx"）
    hyp_id = finding.get("hypothesis_id", "")
    component = hyp_id.split("-")[1] if "-" in hyp_id else "未知组件"

    severity = finding.get("severity", "medium").lower()
    cvss_map = {"critical": 9.5, "high": 8.0, "medium": 5.5, "low": 3.0}

    # 从 created_at (unix timestamp) 转为 YYYY-MM 格式
    ts = finding.get("created_at", 0)
    if ts:
        import datetime
        discovered = datetime.datetime.fromtimestamp(ts).strftime("%Y-%m")
    else:
        discovered = time.strftime("%Y-%m")

    return {
        "id": f"PZ-{finding.get('id', '')}",
        "component": component,
        "version": "",
        "title": finding.get("title", "未命名漏洞"),
        "description": finding.get("description", ""),
        "severity": severity,
        "cvss": cvss_map.get(severity, 5.0),
        "status": "0day",
        "visibility": visibility,
        "discovered_at": discovered,
        "evidence": finding.get("evidence", "")[:200],  # 截断，不泄露完整 PoC
    }


def cmd_push(args):
    """手动推送单条情报"""
    item = {
        "component": args.component,
        "version": args.version,
        "title": args.title,
        "description": args.description,
        "severity": args.severity,
        "cvss": args.cvss,
        "status": args.status,
        "visibility": args.visibility,
        "discovered_at": args.discovered_at or time.strftime("%Y-%m"),
    }
    if args.id:
        item["id"] = args.id

    result = push_items(args.url, args.secret, [item])
    print(f"✅ {result.get('message', 'OK')}")


def cmd_sync(args):
    """从 blackboard.json 批量同步"""
    bb_path = Path(args.blackboard)
    if not bb_path.exists():
        print(f"❌ 找不到 blackboard 文件: {bb_path}")
        sys.exit(1)

    data = json.loads(bb_path.read_text(encoding="utf-8"))
    findings = data.get("findings", [])

    if not findings:
        print("ℹ️  blackboard 中没有 findings，无需同步")
        return

    # 根据 severity 自动决定可见性
    items = []
    for f in findings:
        sev = f.get("severity", "medium").lower()
        # critical/high 默认设为订阅专属；medium/low 默认公开
        vis = "locked" if sev in ("critical", "high") else "public"
        items.append(finding_to_intel(f, visibility=vis))

    print(f"📤 准备推送 {len(items)} 条情报...")
    result = push_items(args.url, args.secret, items)
    print(f"✅ {result.get('message', 'OK')} — 外网总计 {result.get('total', '?')} 条")


def cmd_list(args):
    """列出外网当前情报（调试用）"""
    resp = requests.get(args.url, timeout=15)
    resp.raise_for_status()
    intel = resp.json()
    print(f"📋 外网共 {len(intel)} 条情报:\n")
    for item in intel:
        sev = item.get("severity", "?").upper()
        vis = "🔒" if item.get("visibility") == "locked" else "🌐"
        print(f"  {vis} [{sev}] {item.get('title', '?')}  ({item.get('component', '?')})")
    if not intel:
        print("  (空)")


def main():
    parser = argparse.ArgumentParser(
        prog="sync_intel",
        description="猎零 PreZero — 内网漏洞情报同步推送工具",
    )
    sub = parser.add_subparsers(dest="command", required=True)

    # push 子命令
    p_push = sub.add_parser("push", help="手动推送单条情报")
    p_push.add_argument("--url", required=True, help="外网 API 地址")
    p_push.add_argument("--secret", required=True, help="HMAC 签名密钥")
    p_push.add_argument("--id", default="", help="情报ID（留空自动生成）")
    p_push.add_argument("--component", required=True, help="受影响组件名")
    p_push.add_argument("--version", default="", help="受影响版本")
    p_push.add_argument("--title", required=True, help="漏洞标题")
    p_push.add_argument("--description", default="", help="漏洞描述")
    p_push.add_argument("--severity", default="medium",
                        choices=["critical", "high", "medium", "low"])
    p_push.add_argument("--cvss", type=float, default=0.0, help="CVSS 评分")
    p_push.add_argument("--status", default="0day", help="状态")
    p_push.add_argument("--visibility", default="locked",
                        choices=["public", "locked"])
    p_push.add_argument("--discovered-at", default="", help="发现日期 YYYY-MM")

    # sync 子命令
    p_sync = sub.add_parser("sync", help="从 blackboard.json 批量同步")
    p_sync.add_argument("--url", required=True, help="外网 API 地址")
    p_sync.add_argument("--secret", required=True, help="HMAC 签名密钥")
    p_sync.add_argument("--blackboard", required=True,
                        help="blackboard.json 文件路径")

    # list 子命令
    p_list = sub.add_parser("list", help="列出外网当前情报")
    p_list.add_argument("--url", required=True, help="外网 API 地址")

    args = parser.parse_args()

    if args.command == "push":
        cmd_push(args)
    elif args.command == "sync":
        cmd_sync(args)
    elif args.command == "list":
        cmd_list(args)


if __name__ == "__main__":
    main()
