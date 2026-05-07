#!/usr/bin/env python3
"""
自动下载并安装 OSV-Scanner 到项目本地 bin/ 目录。

用法:
    python tools/install_osv.py
    # 或指定版本
    python tools/install_osv.py --version v2.0.0

安装后可通过以下方式使用:
    export OSV_SCANNER_PATH=./bin/osv-scanner
    python kimi.py scan ./my-project
"""

import argparse
import platform
import sys
import urllib.request
import zipfile
from pathlib import Path

RELEASE_BASE = "https://github.com/google/osv-scanner/releases/download"


def detect_platform() -> tuple[str, str]:
    """检测操作系统和架构，返回 (os_name, arch)。"""
    system = platform.system().lower()
    machine = platform.machine().lower()

    os_name = {"darwin": "darwin", "linux": "linux", "windows": "windows"}.get(system, system)
    arch = {"amd64": "amd64", "x86_64": "amd64", "arm64": "arm64", "aarch64": "arm64"}.get(
        machine, machine
    )

    if os_name not in ("darwin", "linux", "windows"):
        print(f"Unsupported OS: {os_name}", file=sys.stderr)
        sys.exit(1)
    if arch not in ("amd64", "arm64"):
        print(f"Unsupported architecture: {arch}", file=sys.stderr)
        sys.exit(1)

    return os_name, arch


def download_osv_scanner(version: str, dest_dir: Path) -> Path:
    os_name, arch = detect_platform()
    ext = "zip" if os_name == "windows" else "tar.gz"
    asset_name = f"osv-scanner_{version}_{os_name}_{arch}.{ext}"
    url = f"{RELEASE_BASE}/{version}/{asset_name}"

    dest_dir.mkdir(parents=True, exist_ok=True)
    archive_path = dest_dir / asset_name

    print(f"Downloading {asset_name} ...")
    print(f"URL: {url}")

    try:
        urllib.request.urlretrieve(url, archive_path)
    except Exception as exc:
        print(f"Download failed: {exc}", file=sys.stderr)
        sys.exit(1)

    # 解压
    if ext == "zip":
        with zipfile.ZipFile(archive_path, "r") as zf:
            zf.extractall(dest_dir)
    else:
        import tarfile
        with tarfile.open(archive_path, "r:gz") as tf:
            tf.extractall(dest_dir)

    archive_path.unlink()

    # 查找二进制
    binary_name = "osv-scanner.exe" if os_name == "windows" else "osv-scanner"
    candidates = list(dest_dir.rglob(binary_name))
    if not candidates:
        print(f"Binary not found after extraction. Check {dest_dir}", file=sys.stderr)
        sys.exit(1)

    binary = candidates[0]
    final_path = dest_dir / binary_name
    binary.rename(final_path)

    # 清理残留目录（osv-scanner 压缩包通常包含一个子目录）
    for item in dest_dir.iterdir():
        if item.is_dir():
            import shutil
            shutil.rmtree(item)

    # 设置可执行权限
    if os_name != "windows":
        final_path.chmod(0o755)

    return final_path


def main():
    parser = argparse.ArgumentParser(description="Install OSV-Scanner for kimiSec")
    parser.add_argument("--version", default="v2.0.0", help="OSV-Scanner release version")
    parser.add_argument(
        "--dest", default="bin", help="Destination directory (default: ./bin)"
    )
    args = parser.parse_args()

    root = Path(__file__).parent.parent.resolve()
    dest_dir = root / args.dest

    binary = download_osv_scanner(args.version, dest_dir)
    print(f"✅ OSV-Scanner installed: {binary}")
    print(f"   Set environment variable: export OSV_SCANNER_PATH={binary}")


if __name__ == "__main__":
    main()
