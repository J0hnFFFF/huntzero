package report

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"zdll/internal/core"
)

// FindingKey returns a stable, deterministic key for a finding.
// It is used for baseline comparison and deduplication.
func FindingKey(f *core.Finding) string {
	if f == nil {
		return ""
	}

	location := ""
	if f.Location != nil && f.Location.File != "" {
		location = f.Location.File
		if f.Location.Line > 0 {
			location += fmt.Sprintf(":%d", f.Location.Line)
		}
	}
	if location == "" {
		loc := extractLocation(f)
		if loc != nil {
			location = loc.PhysicalLocation.ArtifactLocation.URI
			if loc.PhysicalLocation.Region.StartLine > 0 {
				location += fmt.Sprintf(":%d", loc.PhysicalLocation.Region.StartLine)
			}
		}
	}

	switch f.FindingType {
	case core.FindingTypeDependency:
		if f.CVEID != "" {
			return fmt.Sprintf("dep:cve:%s", strings.ToLower(f.CVEID))
		}
		if f.PackageName != "" {
			key := fmt.Sprintf("dep:pkg:%s", strings.ToLower(f.PackageName))
			if f.PackageVersion != "" {
				key += ":" + strings.ToLower(f.PackageVersion)
			}
			return key
		}
	}

	titleSlug := slugify(f.Title)
	if location != "" {
		return fmt.Sprintf("%s:%s:%s", string(f.FindingType), titleSlug, location)
	}
	return fmt.Sprintf("%s:%s", string(f.FindingType), titleSlug)
}

// CompareFindings returns keys present in current but not baseline (new findings)
// and keys present in baseline but not current (resolved findings).
func CompareFindings(current, baseline []string) (newKeys, removedKeys []string) {
	baseSet := make(map[string]struct{}, len(baseline))
	for _, k := range baseline {
		if k != "" {
			baseSet[k] = struct{}{}
		}
	}
	curSet := make(map[string]struct{}, len(current))
	for _, k := range current {
		if k != "" {
			curSet[k] = struct{}{}
		}
	}

	for k := range curSet {
		if _, ok := baseSet[k]; !ok {
			newKeys = append(newKeys, k)
		}
	}
	for k := range baseSet {
		if _, ok := curSet[k]; !ok {
			removedKeys = append(removedKeys, k)
		}
	}
	return newKeys, removedKeys
}

// LoadBaselineKeys reads a baseline file and returns a list of finding keys.
// The file may be a JSON array of strings or a plain text file with one key per line.
func LoadBaselineKeys(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var keys []string
	if err := json.Unmarshal(data, &keys); err == nil {
		return keys, nil
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			keys = append(keys, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return keys, nil
}

// FindingKeys maps findings to stable keys.
func FindingKeys(findings []*core.Finding) []string {
	keys := make([]string, 0, len(findings))
	for _, f := range findings {
		keys = append(keys, FindingKey(f))
	}
	return keys
}

// SeverityAtOrAbove reports whether any finding has a severity rank >= threshold.
func SeverityAtOrAbove(findings []*core.Finding, threshold string) (bool, int) {
	thresholdRank := core.SeverityRank(threshold)
	count := 0
	for _, f := range findings {
		if core.SeverityRank(f.Severity) >= thresholdRank {
			count++
		}
	}
	return count > 0, count
}

// ParseSeverity validates and normalizes a severity string.
func ParseSeverity(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case core.SeverityCritical, core.SeverityHigh, core.SeverityMedium, core.SeverityLow, core.SeverityNone:
		return s, nil
	default:
		return "", fmt.Errorf("invalid severity %q (want critical, high, medium, low, none)", s)
	}
}

// CountBySeverity returns a severity -> count map.
func CountBySeverity(findings []*core.Finding) map[string]int {
	counts := map[string]int{
		core.SeverityCritical: 0,
		core.SeverityHigh:     0,
		core.SeverityMedium:   0,
		core.SeverityLow:      0,
		core.SeverityNone:     0,
	}
	for _, f := range findings {
		counts[strings.ToLower(f.Severity)]++
	}
	return counts
}

// CountByType returns counts by finding type.
func CountByType(findings []*core.Finding) map[core.FindingType]int {
	counts := map[core.FindingType]int{}
	for _, f := range findings {
		counts[f.FindingType]++
	}
	return counts
}
