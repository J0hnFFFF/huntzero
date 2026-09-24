"""engine/osv_bridge.py OSV 解析与转换（SPEC 5.3）。解析函数全为纯同步。"""

import pytest

import engine.osv_bridge as osv


def _osv_doc(
    severity_db=None, cvss=None, aliases=None, pkg="libpng", version="1.6.0", fixed="1.6.1"
):
    vuln = {
        "id": "OSV-2024-0001",
        "aliases": aliases or ["CVE-2024-0001"],
        "summary": "heap overflow",
        "affected": [
            {
                "package": {"name": pkg},
                "ranges": [{"events": [{"introduced": "0"}, {"fixed": fixed}]}],
            }
        ],
    }
    if severity_db:
        vuln["database_specific"] = {"severity": severity_db}
    if cvss:
        vuln["severity"] = [{"type": "CVSS_V3", "score": cvss}]
    return {
        "results": [
            {
                "source": {"type": "lockfile", "path": "requirements.txt"},
                "packages": [
                    {"package": {"name": pkg, "version": version}, "vulnerabilities": [vuln]}
                ],
            }
        ]
    }


class TestParseOsvJson:
    def test_fields_and_cvss_override(self):
        # CVSS score 按首个 token float 解析（"CVSS:3.1/AV:N/9.8" 向量串会
        # 解析失败回落 database_specific），故用裸分字符串触发覆盖语义。
        result = osv._parse_osv_json(_osv_doc(severity_db="MODERATE", cvss="9.8"), 1.5)
        assert len(result.vulnerabilities) == 1
        v = result.vulnerabilities[0]
        assert v.osv_id == "OSV-2024-0001"
        assert v.severity == "critical"  # CVSS 9.8 覆盖 database_specific 的 moderate
        assert v.cvss_score == pytest.approx(9.8)
        assert v.fixed_version == "1.6.1"
        assert result.scanned_files == ["lockfile:requirements.txt"]

    def test_normalize_and_cvss_mapping(self):
        assert osv._normalize_severity("severe") == "high"
        assert osv._normalize_severity("moderate") == "medium"
        assert osv._normalize_severity("bogus") == "medium"
        assert osv._cvss_to_severity(9.8) == "critical"
        assert osv._cvss_to_severity(7.0) == "high"
        assert osv._cvss_to_severity(4.0) == "medium"
        assert osv._cvss_to_severity(2.0) == "low"


class TestScanDependencies:
    async def test_binary_missing_returns_errors_not_raise(self, monkeypatch, tmp_path):
        monkeypatch.setattr(osv, "_find_osv_scanner", lambda: None)
        result = await osv.scan_dependencies(tmp_path)
        assert result.vulnerabilities == []
        assert any("not found" in e for e in result.errors)


class TestFindingsConversion:
    def test_dedup_and_cve_title(self):
        result = osv._parse_osv_json(_osv_doc(cvss="9.8"), 0.1)
        items = osv.osv_findings_to_blackboard(result)
        assert len(items) == 1
        item = items[0]
        assert item["title"] == "CVE-2024-0001: heap overflow"  # 优先 CVE alias + summary
        assert item["finding_type"] == "dependency_vuln"
        assert item["hypothesis_id"] == "osv-OSV-2024-0001"
        assert item["package_name"] == "libpng"

    def test_context_empty_and_medium_only(self):
        empty = osv.OSVScanResult()
        assert osv.build_cerebrum_context(empty) == ""
        low_only = osv._parse_osv_json(_osv_doc(severity_db="MINOR"), 0.1)
        ctx = osv.build_cerebrum_context(low_only)
        assert "all medium/low severity" in ctx

    def test_context_critical_grouped(self):
        result = osv._parse_osv_json(_osv_doc(cvss="9.8"), 0.1)
        ctx = osv.build_cerebrum_context(result)
        assert "libpng@1.6.0" in ctx and "CVE-2024-0001" in ctx and "IMPORTANT" in ctx
