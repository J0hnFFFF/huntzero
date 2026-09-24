#!/usr/bin/env python3
"""
自动下载并安装 OSV-Scanner 到项目本地 bin/ 目录。

用法:
    python tools/install_osv.py
    # 或指定版本
    python tools/install_osv.py --version v2.3.8

安装后可通过以下方式使用:
    export OSV_SCANNER_PATH=./bin/osv-scanner
    python huntzero.py scan ./my-project
"""

import argparse
import platform
import shutil
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


def _download(url: str, dest: Path) -> None:
    """下载文件到指定路径。"""
    print(f"Downloading from {url} ...")
    try:
        urllib.request.urlretrieve(url, dest)
    except Exception as exc:
        print(f"Download failed: {exc}", file=sys.stderr)
        raise


def download_osv_scanner(version: str, dest_dir: Path) -> Path:
    os_name, arch = detect_platform()
    binary_name = "osv-scanner.exe" if os_name == "windows" else "osv-scanner"
    final_path = dest_dir / binary_name
    dest_dir.mkdir(parents=True, exist_ok=True)

    # ── 策略 1: 新版格式（v2.3.0+）直接发布裸二进制 ──
    # 资产名: osv-scanner_linux_amd64（无版本号前缀，无压缩后缀）
    bare_asset = f"osv-scanner_{os_name}_{arch}"
    if os_name == "windows":
        bare_asset += ".exe"
    bare_url = f"{RELEASE_BASE}/{version}/{bare_asset}"

    try:
        _download(bare_url, final_path)
        if os_name != "windows":
            final_path.chmod(0o755)
        print(f"✅ Downloaded bare binary: {bare_asset}")
        return final_path
    except Exception:
        print(f"   Bare binary not found, falling back to archive format...")

    # ── 策略 2: 旧版格式（v2.0.x 及更早）tar.gz / zip 压缩包 ──
    # 资产名: osv-scanner_v2.0.0_linux_amd64.tar.gz
    ext = "zip" if os_name == "windows" else "tar.gz"
    archive_asset = f"osv-scanner_{version}_{os_name}_{arch}.{ext}"
    archive_url = f"{RELEASE_BASE}/{version}/{archive_asset}"
    archive_path = dest_dir / archive_asset

    try:
        _download(archive_url, archive_path)
    except Exception as exc:
        print(f"Download failed for both formats: {exc}", file=sys.stderr)
        print(f"   Tried: {bare_url}", file=sys.stderr)
        print(f"   Tried: {archive_url}", file=sys.stderr)
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
    candidates = list(dest_dir.rglob(binary_name))
    if not candidates:
        print(f"Binary not found after extraction. Check {dest_dir}", file=sys.stderr)
        sys.exit(1)

    binary = candidates[0]
    binary.rename(final_path)

    # 清理残留目录
    for item in dest_dir.iterdir():
        if item.is_dir():
            shutil.rmtree(item)

    if os_name != "windows":
        final_path.chmod(0o755)

    print(f"✅ Downloaded and extracted: {archive_asset}")
    return final_path


def main():
    parser = argparse.ArgumentParser(description="Install OSV-Scanner for huntzero")
    parser.add_argument(
        "--version",
        default="v2.3.8",
        help="OSV-Scanner release version (default: v2.3.8)",
    )
    parser.add_argument(
        "--dest", default="bin", help="Destination directory (default: ./bin)"
    )
    args = parser.parse_args()

    root = Path(__file__).parent.parent.resolve()
    dest_dir = root / args.dest

    binary = download_osv_scanner(args.version, dest_dir)
    print(f"\n✅ OSV-Scanner installed: {binary}")
    print(f"   Set environment variable: export OSV_SCANNER_PATH={binary}")


if __name__ == "__main__":
    main()
