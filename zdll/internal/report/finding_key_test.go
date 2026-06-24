package report

import (
	"testing"

	"zdll/internal/core"
)

func TestFindingKey_Dependency(t *testing.T) {
	f := &core.Finding{
		FindingType:    core.FindingTypeDependency,
		CVEID:          "CVE-2024-0001",
		PackageName:    "lodash",
		PackageVersion: "4.17.20",
	}
	if got := FindingKey(f); got != "dep:cve:cve-2024-0001" {
		t.Errorf("key = %q, want dep:cve:cve-2024-0001", got)
	}
}

func TestFindingKey_ZeroDayLocation(t *testing.T) {
	f := &core.Finding{
		FindingType: core.FindingTypeZeroDay,
		Title:       "SQL injection",
		Evidence:    "auth.go:10 unsafely builds query",
		Severity:    core.SeverityHigh,
	}
	if got := FindingKey(f); got != "zero_day:sql_injection:auth.go:10" {
		t.Errorf("key = %q, want zero_day:sql_injection:auth.go:10", got)
	}
}

func TestCompareFindings(t *testing.T) {
	current := []string{"a", "b", "c"}
	baseline := []string{"b", "c", "d"}
	newKeys, removedKeys := CompareFindings(current, baseline)
	if len(newKeys) != 1 || newKeys[0] != "a" {
		t.Errorf("new = %v, want [a]", newKeys)
	}
	if len(removedKeys) != 1 || removedKeys[0] != "d" {
		t.Errorf("removed = %v, want [d]", removedKeys)
	}
}

func TestFindingKey_LocationField(t *testing.T) {
	f := &core.Finding{
		FindingType: core.FindingTypeZeroDay,
		Title:       "Hardcoded secret",
		Location:    &core.Location{File: "/app/config.yaml", Line: 5},
	}
	if got := FindingKey(f); got != "zero_day:hardcoded_secret:/app/config.yaml:5" {
		t.Errorf("key = %q, want zero_day:hardcoded_secret:/app/config.yaml:5", got)
	}
}

func TestSeverityAtOrAbove(t *testing.T) {
	findings := []*core.Finding{
		{Severity: core.SeverityMedium},
		{Severity: core.SeverityHigh},
		{Severity: core.SeverityLow},
	}
	failed, count := SeverityAtOrAbove(findings, core.SeverityHigh)
	if !failed || count != 1 {
		t.Errorf("failed=%v count=%d, want true/1", failed, count)
	}
	failed, count = SeverityAtOrAbove(findings, core.SeverityCritical)
	if failed || count != 0 {
		t.Errorf("failed=%v count=%d, want false/0", failed, count)
	}
}
