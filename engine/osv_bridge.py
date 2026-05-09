"""
OSV-Scanner Bridge — 将 Google OSV-Scanner 的依赖漏洞扫描结果
无缝整合进 kimiSec 的 Blackboard / Cerebrum 管线。

职责：
  1. 探测/调用 osv-scanner 二进制
  2. 解析 JSON 输出，转换为 Finding 对象
  3. 为 Cerebrum 生成高信噪比的上下文摘要
  4. 完全可选：二进制缺失时静默跳过，不阻塞主流程
"""

import asyncio
import json
import logging
import os
import shutil
import sys
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Optional

logger = logging.getLogger(__name__)

# ─────────────────────────────────────────────────────────────────────────────
#  数据模型
# ─────────────────────────────────────────────────────────────────────────────


@dataclass
class OSVVulnerability:
    """单个 OSV 依赖漏洞的规范化表示。"""

    osv_id: str
    aliases: list[str] = field(default_factory=list)          # CVE-202X-XXXX, GHSA-xxx
    summary: str = ""
    details: str = ""
    severity: str = "medium"                                   # critical | high | medium | low
    cvss_score: Optional[float] = None
    package_name: str = ""
    package_version: str = ""
    fixed_version: Optional[str] = None
    ecosystem: str = ""                                        # npm, PyPI, Go, Maven...
    source_path: str = ""                                      # 哪个 lockfile/manifest
    is_reachable: Optional[bool] = None                        # Call analysis 结果


@dataclass
class OSVScanResult:
    """一次 OSV 扫描的完整结果。"""

    vulnerabilities: list[OSVVulnerability] = field(default_factory=list)
    scanned_files: list[str] = field(default_factory=list)
    errors: list[str] = field(default_factory=list)
    elapsed_seconds: float = 0.0

    @property
    def critical_count(self) -> int:
        return sum(1 for v in self.vulnerabilities if v.severity == "critical")

    @property
    def high_count(self) -> int:
        return sum(1 for v in self.vulnerabilities if v.severity == "high")

    @property
    def medium_count(self) -> int:
        return sum(1 for v in self.vulnerabilities if v.severity == "medium")

    @property
    def low_count(self) -> int:
        return sum(1 for v in self.vulnerabilities if v.severity == "low")


# ─────────────────────────────────────────────────────────────────────────────
#  二进制探测
# ─────────────────────────────────────────────────────────────────────────────


def _find_osv_scanner() -> Optional[str]:
    """查找 osv-scanner 可执行文件路径。"""
    # 1. 环境变量显式指定
    env_path = os.environ.get("OSV_SCANNER_PATH")
    if env_path and Path(env_path).exists():
        return str(Path(env_path).resolve())

    # 2. 项目本地 bin/ 目录（install_osv.py 默认安装位置）——优先于 PATH，避免系统旧版冲突
    local_bin = Path(__file__).parent.parent / "bin" / "osv-scanner"
    if sys.platform == "win32":
        local_bin = local_bin.with_suffix(".exe")
    if local_bin.exists():
        return str(local_bin)

    # 3. PATH 中查找（可能找到系统包管理器安装的旧版）
    found = shutil.which("osv-scanner")
    if found:
        return found

    return None


# ─────────────────────────────────────────────────────────────────────────────
#  扫描执行
# ─────────────────────────────────────────────────────────────────────────────


async def scan_dependencies(
    target: Path,
    *,
    recursive: bool = True,
    offline: bool = False,
    timeout: float = 300.0,
) -> OSVScanResult:
    """
    对目标目录执行 OSV-Scanner 依赖扫描。

    Args:
        target: 项目根目录或 lockfile 路径。
        recursive: 是否递归扫描子目录。
        offline: 是否使用离线模式（需预先下载数据库）。
        timeout: 扫描超时（秒）。

    Returns:
        OSVScanResult，即使出错也返回空结果对象，不抛异常。
    """
    binary = _find_osv_scanner()
    if not binary:
        logger.warning("osv-scanner not found. Skipping dependency audit.")
        return OSVScanResult(errors=["osv-scanner binary not found"])

    cmd = [binary, "scan", "source"]
    if recursive:
        cmd.append("-r")
    if offline:
        cmd.append("--offline")
    cmd.extend(["--format", "json", str(target)])

    logger.info(f"Running OSV-Scanner: {' '.join(cmd)}")
    start = asyncio.get_event_loop().time()

    try:
        proc = await asyncio.create_subprocess_exec(
            *cmd,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )
        stdout, stderr = await asyncio.wait_for(proc.communicate(), timeout=timeout)
    except asyncio.TimeoutError:
        logger.warning("OSV-Scanner timed out.")
        try:
            proc.kill()
            await proc.communicate()
        except Exception:
            pass
        return OSVScanResult(errors=["osv-scanner timed out"])
    except Exception as exc:
        logger.warning(f"OSV-Scanner execution failed: {exc}")
        return OSVScanResult(errors=[f"execution error: {exc}"])

    elapsed = asyncio.get_event_loop().time() - start

    # osv-scanner 退出码 0 = 无漏洞，1 = 发现漏洞，>1 = 错误
    if proc.returncode > 1:
        err_text = stderr.decode("utf-8", errors="ignore").strip()
        logger.warning(f"OSV-Scanner exited {proc.returncode}: {err_text}")
        return OSVScanResult(errors=[f"exit code {proc.returncode}: {err_text}"])

    if not stdout:
        return OSVScanResult(elapsed_seconds=elapsed)

    # 解析 JSON
    try:
        data = json.loads(stdout.decode("utf-8", errors="ignore"))
    except json.JSONDecodeError as exc:
        logger.warning(f"Failed to parse OSV-Scanner JSON: {exc}")
        return OSVScanResult(errors=[f"json parse error: {exc}"])

    return _parse_osv_json(data, elapsed)


# ─────────────────────────────────────────────────────────────────────────────
#  结果解析
# ─────────────────────────────────────────────────────────────────────────────


def _parse_osv_json(data: dict, elapsed: float) -> OSVScanResult:
    """将 osv-scanner --format json 的输出解析为 OSVScanResult。"""
    result = OSVScanResult(elapsed_seconds=elapsed)

    # osv-scanner v2 JSON 结构: { "results": [ { "source": ..., "packages": [ ... ] } ] }
    for source_result in data.get("results", []):
        source_path = source_result.get("source", {}).get("path", "")
        source_type = source_result.get("source", {}).get("type", "")
        result.scanned_files.append(f"{source_type}:{source_path}")

        for pkg in source_result.get("packages", []):
            pkg_name = pkg.get("package", {}).get("name", "")
            pkg_version = pkg.get("package", {}).get("version", "")
            ecosystem = pkg.get("package", {}).get("ecosystem", "")

            for vuln in pkg.get("vulnerabilities", []):
                # severity 提取：优先 database_specific.severity，其次 CVSS
                severity = "medium"
                cvss_score: Optional[float] = None

                db_specific = vuln.get("database_specific", {})
                if "severity" in db_specific:
                    severity = _normalize_severity(db_specific["severity"])

                for sev in vuln.get("severity", []):
                    if sev.get("type") == "CVSS_V3":
                        score = sev.get("score", "")
                        if isinstance(score, (int, float)):
                            cvss_score = float(score)
                        else:
                            # 尝试从字符串提取第一个数字
                            try:
                                cvss_score = float(str(score).split()[0])
                            except ValueError:
                                pass
                        if cvss_score is not None:
                            severity = _cvss_to_severity(cvss_score)
                        break

                # fixed version
                fixed = None
                for aff in vuln.get("affected", []):
                    if aff.get("package", {}).get("name") == pkg_name:
                        for rng in aff.get("ranges", []):
                            for ev in rng.get("events", []):
                                if "fixed" in ev:
                                    fixed = ev["fixed"]
                                    break

                # aliases (CVE / GHSA)
                aliases = [a for a in vuln.get("aliases", []) if a]

                # summary / details
                summary = vuln.get("summary") or vuln.get("id", "")
                details = vuln.get("details", "")

                result.vulnerabilities.append(
                    OSVVulnerability(
                        osv_id=vuln.get("id", ""),
                        aliases=aliases,
                        summary=summary,
                        details=details,
                        severity=severity,
                        cvss_score=cvss_score,
                        package_name=pkg_name,
                        package_version=pkg_version,
                        fixed_version=fixed,
                        ecosystem=ecosystem,
                        source_path=source_path,
                    )
                )

    return result


# ─────────────────────────────────────────────────────────────────────────────
#  辅助： severity 映射
# ─────────────────────────────────────────────────────────────────────────────


def _normalize_severity(raw: str) -> str:
    m = raw.lower().strip()
    if m in ("critical", "high", "medium", "low"):
        return m
    if m in ("severe", "important"):
        return "high"
    if m in ("moderate"):
        return "medium"
    if m in ("minor", "informational"):
        return "low"
    return "medium"


def _cvss_to_severity(score: float) -> str:
    if score >= 9.0:
        return "critical"
    if score >= 7.0:
        return "high"
    if score >= 4.0:
        return "medium"
    return "low"


# ─────────────────────────────────────────────────────────────────────────────
#  与 Blackboard 的桥接
# ─────────────────────────────────────────────────────────────────────────────


def osv_findings_to_blackboard(result: OSVScanResult) -> list[dict]:
    """
    将 OSVScanResult 转换为 Blackboard.add_finding 所需的 kwargs 列表。

    每个 dict 可直接解包传给 add_finding：
        await bb.add_finding(**item)
    """
    findings = []
    seen_ids: set[str] = set()

    for v in result.vulnerabilities:
        # 去重：同一 OSV ID + 同一包只报一次
        dedup_key = f"{v.osv_id}@{v.package_name}"
        if dedup_key in seen_ids:
            continue
        seen_ids.add(dedup_key)

        # 标题：优先用 CVE ID，否则 OSV ID
        primary_id = next((a for a in v.aliases if a.startswith("CVE-")), v.osv_id)
        title = f"{primary_id}: {v.summary}" if v.summary else primary_id

        # 描述
        desc_lines = [
            f"**Package**: `{v.package_name}@{v.package_version}` ({v.ecosystem})",
            f"**OSV ID**: {v.osv_id}",
        ]
        if v.aliases:
            desc_lines.append(f"**Aliases**: {', '.join(v.aliases)}")
        if v.fixed_version:
            desc_lines.append(f"**Fixed in**: `{v.fixed_version}`")
        if v.cvss_score is not None:
            desc_lines.append(f"**CVSS**: {v.cvss_score}")
        if v.details:
            desc_lines.append(f"\n{v.details}")

        description = "\n".join(desc_lines)

        # 证据
        evidence = f"Source: {v.source_path}\nAffected: {v.package_name}@{v.package_version}"
        if v.fixed_version:
            evidence += f"\nFixed: {v.fixed_version}"

        findings.append({
            "hypothesis_id": f"osv-{v.osv_id}",
            "title": title,
            "description": description,
            "severity": v.severity,
            "evidence": evidence,
            "finding_type": "dependency_vuln",
            "cve_id": primary_id if primary_id.startswith("CVE-") else "",
            "package_name": v.package_name,
            "package_version": v.package_version,
            "fixed_version": v.fixed_version or "",
        })

    return findings


# ─────────────────────────────────────────────────────────────────────────────
#  为 Cerebrum 生成上下文摘要
# ─────────────────────────────────────────────────────────────────────────────


def build_cerebrum_context(result: OSVScanResult, max_entries: int = 15) -> str:
    """
    将 OSV 结果压缩为一段可供 Cerebrum prompt 注入的上下文文本。

    只保留 critical/high 漏洞，按 package 聚合，避免淹没 LLM 上下文。
    """
    if not result.vulnerabilities:
        return ""

    # 仅保留 high/critical，按包聚合
    filtered = [v for v in result.vulnerabilities if v.severity in ("critical", "high")]
    filtered.sort(key=lambda v: ({"critical": 0, "high": 1}.get(v.severity, 2), v.package_name))

    if not filtered:
        return (
            f"[DEPENDENCY AUDIT] {len(result.vulnerabilities)} known vulnerabilities found "
            f"(all medium/low severity). No immediate high-risk dependencies."
        )

    lines = [
        f"[DEPENDENCY AUDIT] {len(filtered)} high/critical known vulnerabilities detected.",
        "The following packages have published CVEs. Use this as PRIORITY guidance:",
        "",
    ]

    # 按包聚合
    by_pkg: dict[str, list[OSVVulnerability]] = {}
    for v in filtered[:max_entries]:
        by_pkg.setdefault(v.package_name, []).append(v)

    for pkg_name, vulns in by_pkg.items():
        versions = {v.package_version for v in vulns}
        version_str = ", ".join(sorted(versions))
        cves = []
        for v in vulns:
            cves.extend(v.aliases)
        cve_str = ", ".join(sorted(set(cves))[:5])  # 最多 5 个 CVE
        lines.append(f"- {pkg_name}@{version_str}: {cve_str}")

    if len(filtered) > max_entries:
        lines.append(f"\n... and {len(filtered) - max_entries} more high/critical issues.")

    lines.append(
        "\nIMPORTANT: These are KNOWN vulnerabilities in dependencies. "
        "Your job is to determine if the application code EXPLOITS or AMPLIFIES them, "
        "or if there are UNKNOWN vulnerabilities in how these dependencies are USED."
    )

    return "\n".join(lines)
