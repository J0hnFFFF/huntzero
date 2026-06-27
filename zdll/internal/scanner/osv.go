package scanner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"zdll/internal/config"
	"zdll/internal/core"
)

// DefaultOSVTimeout is the maximum time allowed for an OSV scan.
const DefaultOSVTimeout = 300 * time.Second

// OSVScanner wraps the Google osv-scanner binary.
type OSVScanner struct {
	Path    string
	Offline bool
}

// NewOSVScanner creates an OSVScanner that uses the provided binary path.
// By default it runs in online mode (matching the Python engine); set Offline
// to true to pass --offline to osv-scanner.
func NewOSVScanner(path string) *OSVScanner {
	return &OSVScanner{Path: path, Offline: false}
}

// Name returns the scanner identifier.
func (s *OSVScanner) Name() string { return "osv" }

// Scan runs osv-scanner against target and returns dependency findings.
// The binary must already be present on the system; this package does not
// auto-download executables to avoid supply-chain risk.
func (s *OSVScanner) Scan(ctx context.Context, target string) ([]*core.Finding, error) {
	binary := s.Path
	if binary == "" {
		var ok bool
		var err error
		// Try to discover the binary, including the target project's bin/ dir.
		binary, ok, err = resolveOSVScanner(nil, target)
		if err != nil {
			return nil, fmt.Errorf("osv-scanner not available: %w", err)
		}
		if !ok {
			log.Printf("warning: osv-scanner not found; install it from https://github.com/google/osv-scanner/releases or set OSV_SCANNER_PATH")
			return []*core.Finding{}, nil
		}
	}

	ctx, cancel := context.WithTimeout(ctx, DefaultOSVTimeout)
	defer cancel()

	args := []string{"scan", "source", "-r", "--format", "json", target}
	if s.Offline {
		args = append(args[:3], append([]string{"--offline"}, args[3:]...)...)
	}
	cmd := exec.CommandContext(ctx, binary, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("osv-scanner timed out after %s", DefaultOSVTimeout)
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() <= 1 {
			// osv-scanner exit 0 = no vulnerabilities, 1 = vulnerabilities found.
		} else {
			return nil, fmt.Errorf("osv-scanner execution failed: %w (stderr: %s)", err, stderr.String())
		}
	}

	if stdout.Len() == 0 {
		return []*core.Finding{}, nil
	}

	var output osvOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		return nil, fmt.Errorf("failed to parse OSV-Scanner JSON: %w", err)
	}

	return convertOSVOutput(ctx, output), nil
}

// resolveOSVScanner discovers the osv-scanner binary using, in order:
//  1. OSV_SCANNER_PATH environment variable
//  2. Configured path from cfg.Paths.OSVScanner
//  3. Each extra root's bin/osv-scanner (or .exe on Windows)
//  4. PATH lookup via exec.LookPath
//
// This function intentionally does not auto-download binaries. See the audit
// remediation note in osv.go for the rationale.
func resolveOSVScanner(cfg *config.Config, roots ...string) (string, bool, error) {
	binaryName := "osv-scanner"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}

	if envPath := os.Getenv("OSV_SCANNER_PATH"); envPath != "" {
		if isExecutableFile(envPath) {
			return envPath, true, nil
		}
	}

	if cfg != nil && cfg.Paths.OSVScanner != "" {
		if isExecutableFile(cfg.Paths.OSVScanner) {
			return cfg.Paths.OSVScanner, true, nil
		}
	}

	for _, root := range roots {
		if root == "" {
			continue
		}
		candidate := filepath.Join(root, "bin", binaryName)
		if isExecutableFile(candidate) {
			return candidate, true, nil
		}
	}

	if found, err := exec.LookPath(binaryName); err == nil {
		return found, true, nil
	}

	return "", false, nil
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}



// ---------------------------------------------------------------------------
// JSON models matching osv-scanner --format json output.
// ---------------------------------------------------------------------------

type osvOutput struct {
	Results []osvResult `json:"results"`
}

type osvResult struct {
	Source   osvSource    `json:"source"`
	Packages []osvPackage `json:"packages"`
}

type osvSource struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

type osvPackage struct {
	Package         osvPackageInfo `json:"package"`
	Vulnerabilities []osvVuln      `json:"vulnerabilities"`
}

type osvPackageInfo struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Ecosystem string `json:"ecosystem"`
}

type osvVuln struct {
	ID               string                 `json:"id"`
	Aliases          []string               `json:"aliases"`
	Summary          string                 `json:"summary"`
	Details          string                 `json:"details"`
	DatabaseSpecific map[string]interface{} `json:"database_specific"`
	Severity         []osvSeverity          `json:"severity"`
	Affected         []osvAffected          `json:"affected"`
}

type osvSeverity struct {
	Type  string `json:"type"`
	Score any    `json:"score"`
}

type osvAffected struct {
	Package osvPackageInfo `json:"package"`
	Ranges  []osvRange     `json:"ranges"`
}

type osvRange struct {
	Events []map[string]string `json:"events"`
}

// ---------------------------------------------------------------------------
// Conversion to core.Finding.
// ---------------------------------------------------------------------------

func convertOSVOutput(ctx context.Context, output osvOutput) []*core.Finding {
	seen := make(map[string]struct{})
	var findings []*core.Finding

	for _, result := range output.Results {
		sourcePath := result.Source.Path
		if !core.IsPathInChangedPaths(ctx, sourcePath) {
			continue
		}
		for _, pkg := range result.Packages {
			pkgInfo := pkg.Package
			for _, vuln := range pkg.Vulnerabilities {
				key := fmt.Sprintf("%s@%s", vuln.ID, pkgInfo.Name)
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}

				severity, cvssScore := extractSeverity(vuln)
				fixedVersion := extractFixedVersion(vuln, pkgInfo.Name)
				aliases := filterAliases(vuln.Aliases)
				cveID := firstCVEAlias(aliases, vuln.ID)

				title := buildTitle(cveID, vuln.ID, vuln.Summary)
				description := buildDescription(pkgInfo, vuln, aliases, fixedVersion, cvssScore)
				evidence := buildEvidence(sourcePath, pkgInfo, fixedVersion)

				finding := core.NewFinding(
					fmt.Sprintf("osv-%s", vuln.ID),
					title,
					description,
					severity,
					evidence,
				)
				finding.FindingType = core.FindingTypeDependency
				finding.CVEID = cveID
				finding.PackageName = pkgInfo.Name
				finding.PackageVersion = pkgInfo.Version
				finding.FixedVersion = fixedVersion
				finding.Location = &core.Location{File: sourcePath}

				findings = append(findings, finding)
			}
		}
	}

	return findings
}

func extractSeverity(vuln osvVuln) (string, *float64) {
	if raw, ok := vuln.DatabaseSpecific["severity"].(string); ok && raw != "" {
		return normalizeSeverity(raw), nil
	}

	for _, sev := range vuln.Severity {
		if sev.Type == "CVSS_V3" {
			score := parseCVSSScore(sev.Score)
			if score != nil {
				return cvssToSeverity(*score), score
			}
		}
	}

	return "medium", nil
}

func parseCVSSScore(score any) *float64 {
	var f float64
	switch s := score.(type) {
	case float64:
		f = s
	case float32:
		f = float64(s)
	case int:
		f = float64(s)
	case int64:
		f = float64(s)
	case string:
		parts := strings.Fields(s)
		if len(parts) == 0 {
			return nil
		}
		parsed, err := strconv.ParseFloat(parts[0], 64)
		if err != nil {
			return nil
		}
		f = parsed
	default:
		return nil
	}
	return &f
}

func normalizeSeverity(raw string) string {
	m := strings.ToLower(strings.TrimSpace(raw))
	switch m {
	case "critical", "high", "medium", "low":
		return m
	case "severe", "important":
		return "high"
	case "moderate":
		return "medium"
	case "minor", "informational":
		return "low"
	default:
		return "medium"
	}
}

func cvssToSeverity(score float64) string {
	switch {
	case score >= 9.0:
		return "critical"
	case score >= 7.0:
		return "high"
	case score >= 4.0:
		return "medium"
	default:
		return "low"
	}
}

func extractFixedVersion(vuln osvVuln, pkgName string) string {
	for _, aff := range vuln.Affected {
		if aff.Package.Name != pkgName {
			continue
		}
		for _, rng := range aff.Ranges {
			for _, ev := range rng.Events {
				if fixed, ok := ev["fixed"]; ok && fixed != "" {
					return fixed
				}
			}
		}
	}
	return ""
}

func filterAliases(aliases []string) []string {
	out := make([]string, 0, len(aliases))
	for _, a := range aliases {
		if a != "" {
			out = append(out, a)
		}
	}
	return out
}

func firstCVEAlias(aliases []string, osvID string) string {
	for _, a := range aliases {
		if strings.HasPrefix(a, "CVE-") {
			return a
		}
	}
	return osvID
}

func buildTitle(cveID, osvID, summary string) string {
	primary := cveID
	if primary == "" {
		primary = osvID
	}
	if summary != "" {
		return fmt.Sprintf("%s: %s", primary, summary)
	}
	return primary
}

func buildDescription(pkg osvPackageInfo, vuln osvVuln, aliases []string, fixed string, cvss *float64) string {
	lines := []string{
		fmt.Sprintf("**Package**: `%s@%s` (%s)", pkg.Name, pkg.Version, pkg.Ecosystem),
		fmt.Sprintf("**OSV ID**: %s", vuln.ID),
	}
	if len(aliases) > 0 {
		lines = append(lines, fmt.Sprintf("**Aliases**: %s", strings.Join(aliases, ", ")))
	}
	if fixed != "" {
		lines = append(lines, fmt.Sprintf("**Fixed in**: `%s`", fixed))
	}
	if cvss != nil {
		lines = append(lines, fmt.Sprintf("**CVSS**: %.1f", *cvss))
	}
	if vuln.Details != "" {
		lines = append(lines, "", vuln.Details)
	}
	return strings.Join(lines, "\n")
}

func buildEvidence(sourcePath string, pkg osvPackageInfo, fixed string) string {
	evidence := fmt.Sprintf("Source: %s\nAffected: %s@%s", sourcePath, pkg.Name, pkg.Version)
	if fixed != "" {
		evidence += fmt.Sprintf("\nFixed: %s", fixed)
	}
	return evidence
}
