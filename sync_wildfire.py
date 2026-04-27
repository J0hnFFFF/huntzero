"""
sync_wildfire.py — 在野漏洞情报(Wildfire)专栏推送工具。

将本地的 Markdown 文件推送至外网官网 (website_server.py) 的 /api/wildfire 接口，
以用于在野漏洞情报版面的展示。

由于 /api/wildfire 接口目前复用了与 /api/intel 一样的 HMAC-SHA256 签名校验（如果提供），
我们在此工具中加入签名支持，确保推送接口安全。

── 使用方式 ──

  1. 推送单个 Markdown 文章：
  python sync_wildfire.py push \
      --url http://localhost:8000/api/wildfire \
      --secret "your-shared-secret" \
      --title "泛微 OA 最新 RCE 漏洞" \
      --md-file ./reports/sample.md \
      --author "Hive-Mind INTEL"

  2. 查看当前专栏的文章列表：
  python sync_wildfire.py list --url http://localhost:8000/api/wildfire
"""

import argparse
import hashlib
import hmac
import json
import sys
from pathlib import Path

try:
    import requests
except ImportError:
    print("需要安装 requests: pip install requests")
    sys.exit(1)


def sign(payload: bytes, secret: str) -> str:
    """生成 HMAC-SHA256 签名"""
    return hmac.new(secret.encode(), payload, hashlib.sha256).hexdigest()


def push_wildfire_article(api_url: str, secret: str, article: dict) -> dict:
    """
    推送到外网 /api/wildfire
    """
    payload_bytes = json.dumps(article, ensure_ascii=False).encode("utf-8")
    headers = {"Content-Type": "application/json"}
    
    if secret:
        signature = sign(payload_bytes, secret)
        headers["X-Signature"] = signature

    resp = requests.post(
        api_url,
        data=payload_bytes,
        headers=headers,
        timeout=30,
    )
    
    if not resp.ok:
        try:
            err = resp.json()
            print(f"❌ 推送失败 [{resp.status_code}]: {err}")
        except:
            print(f"❌ 推送失败 [{resp.status_code}]: {resp.text}")
        sys.exit(1)
        
    return resp.json()


def cmd_push(args):
    md_path = Path(args.md_file)
    if not md_path.exists():
        print(f"❌ 找不到 Markdown 文件: {md_path}")
        sys.exit(1)
        
    content_md = md_path.read_text(encoding="utf-8")
    
    article = {
        "title": args.title,
        "content_md": content_md,
        "author": args.author,
    }
    # 如果指定了自定义日期
    if getattr(args, 'date', None):
        article["date"] = args.date
        
    print(f"📤 准备推送文章: 【{args.title}】 (字数: {len(content_md)})")
    
    result = push_wildfire_article(args.url, args.secret, article)
    print(f"✅ {result.get('message', '推送成功')} (ID: {result.get('id', 'unknown')})")


def cmd_list(args):
    resp = requests.get(args.url, timeout=15)
    resp.raise_for_status()
    articles = resp.json()
    
    print(f"📋 共计 {len(articles)} 篇在野情报文章:\n")
    for a in articles:
        print(f"  - [{a.get('date', '?')}] {a.get('title', '?')} (作者: {a.get('author', '?')}) | ID: {a.get('id', '?')}")
    if not articles:
        print("  (空)")


def main():
    parser = argparse.ArgumentParser(
        prog="sync_wildfire",
        description="猎零 PreZero — 在野漏洞情报(Wildfire)专栏推送工具",
    )
    sub = parser.add_subparsers(dest="command", required=True)

    # push 子命令
    p_push = sub.add_parser("push", help="推送本地 Markdown 文章")
    p_push.add_argument("--url", required=True, help="外网 API 地址, 例如: http://127.0.0.1:8000/api/wildfire")
    p_push.add_argument("--secret", default="", help="HMAC 签名密钥 (如配置了)")
    p_push.add_argument("--title", required=True, help="文章标题")
    p_push.add_argument("--md-file", required=True, help="Markdown 文件路径")
    p_push.add_argument("--author", default="Hive-Mind INTEL", help="作者名称")
    p_push.add_argument("--date", default="", help="自定义发布日期 YYYY-MM-DD")

    # list 子命令
    p_list = sub.add_parser("list", help="查看目前已部署的文章列表")
    p_list.add_argument("--url", required=True, help="外网 API 地址")

    args = parser.parse_args()

    if args.command == "push":
        cmd_push(args)
    elif args.command == "list":
        cmd_list(args)

if __name__ == "__main__":
    main()
