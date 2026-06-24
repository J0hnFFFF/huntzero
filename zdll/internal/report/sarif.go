package report

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"zdll/internal/core"
)

// SARIFRenderer produces a SARIF 2.1.0 report from a Blackboard.
// It is intentionally self-contained so that CI systems can consume
// results without extra tooling.
type SARIFRenderer struct{}

func (r *SARIFRenderer) Name() string { return "sarif" }

func (r *SARIFRenderer) Extension() string { return "sarif.json" }

func (r *SARIFRenderer) Render(bb *core.Blackboard, target string, elapsed time.Duration) ([]byte, error) {
	findings := bb.SortedFindings()

	ruleSet := make(map[string]sarifRule)
	var results []sarifResult

	for _, f := range findings {
		ruleID := r.ruleID(f)
		if _, ok := ruleSet[ruleID]; !ok {
			ruleSet[ruleID] = sarifRule{
				ID:   ruleID,
				Name: f.Title,
				ShortDescription: sarifMessage{
					Text: f.Title,
				},
				FullDescription: sarifMessage{
					Text: firstLine(f.Description),
				},
				DefaultConfiguration: sarifRuleConfiguration{
					Level: severityToSARIFLevel(f.Severity),
				},
			}
		}

		loc := extractLocation(f)
		var locations []sarifLocation
		if loc != nil {
			locations = append(locations, *loc)
		}

		results = append(results, sarifResult{
			RuleID:    ruleID,
			Level:     severityToSARIFLevel(f.Severity),
			Message:   sarifMessage{Text: resultMessage(f)},
			Locations: locations,
			Properties: map[string]any{
				"finding_id":      f.ID,
				"hypothesis_id":   f.HypothesisID,
				"finding_type":    f.FindingType,
				"confidence":      "unknown",
				"cve_id":          f.CVEID,
				"package_name":    f.PackageName,
				"package_version": f.PackageVersion,
				"fixed_version":   f.FixedVersion,
			},
		})
	}

	rules := make([]sarifRule, 0, len(ruleSet))
	for _, rule := range ruleSet {
		rules = append(rules, rule)
	}

	doc := sarifDocument{
		Version: "2.1.0",
		Schema:  "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
		Runs: []sarifRun{
			{
				Tool: sarifTool{
					Driver: sarifDriver{
						Name:           "zdll",
						InformationURI: "https://github.com/kimisec/zdll",
						Version:        "0.1.0",
						Rules:          rules,
					},
				},
				Results: results,
				Invocations: []sarifInvocation{
					{
						ExecutionSuccessful: true,
						ExitCode:            0,
					},
				},
			},
		},
	}

	return json.MarshalIndent(doc, "", "  ")
}

func (r *SARIFRenderer) ruleID(f *core.Finding) string {
	if f.CVEID != "" {
		return slugify(f.CVEID)
	}
	if f.FindingType == core.FindingTypeDependency {
		return "DEPENDENCY_VULNERABILITY"
	}
	return "ZD_" + slugify(f.Title)
}

// severityToSARIFLevel maps zdll severity to SARIF level.
func ExtractLocationFromEvidence(evidence string) *core.Location {
	loc := extractLocation(&core.Finding{Evidence: evidence})
	if loc == nil {
		return nil
	}
	return &core.Location{
		File:   loc.PhysicalLocation.ArtifactLocation.URI,
		Line:   loc.PhysicalLocation.Region.StartLine,
		Column: loc.PhysicalLocation.Region.StartColumn,
	}
}

func severityToSARIFLevel(severity string) string {
	switch strings.ToLower(severity) {
	case "critical", "high":
		return "error"
	case "medium":
		return "warning"
	case "low":
		return "note"
	default:
		return "warning"
	}
}

// resultMessage builds a concise SARIF result message.
func resultMessage(f *core.Finding) string {
	parts := []string{f.Title}
	if f.Description != "" {
		parts = append(parts, "\n"+strings.TrimSpace(f.Description))
	}
	if f.Evidence != "" {
		parts = append(parts, "\nEvidence:\n"+strings.TrimSpace(f.Evidence))
	}
	return strings.Join(parts, "")
}

// extractLocation performs best-effort extraction of file/line/column from a Finding.
// It prefers the structured Location field and falls back to parsing evidence.
func extractLocation(f *core.Finding) *sarifLocation {
	if f.Location != nil && f.Location.File != "" {
		return locationFromFileWithColumn(f.Location.File, f.Location.Line, f.Location.Column)
	}

	if f.Evidence == "" {
		return nil
	}

	// Pattern 1: explicit "Source: <path>" used by OSV scanner.
	if strings.HasPrefix(f.Evidence, "Source:") {
		line := strings.SplitN(f.Evidence, "\n", 2)[0]
		line = strings.TrimPrefix(line, "Source:")
		file := strings.TrimSpace(line)
		if file != "" {
			return locationFromFile(file, 1)
		}
	}

	// Pattern 2: generic "file:line" in evidence.
	re := regexp.MustCompile(`([\w\.\-\\/]+\.(?:go|py|js|ts|java|c|cpp|h|hpp|rs|rb|kt|swift|php)):(\d+)`)
	if m := re.FindStringSubmatch(f.Evidence); len(m) == 3 {
		line, _ := strconv.Atoi(m[2])
		return locationFromFile(m[1], line)
	}

	// Pattern 3: any path-like string followed by :line.
	re2 := regexp.MustCompile(`([\w\.\-\\/]+\.\w+):(\d+)`)
	if m := re2.FindStringSubmatch(f.Evidence); len(m) == 3 {
		line, _ := strconv.Atoi(m[2])
		return locationFromFile(m[1], line)
	}

	return nil
}

func locationFromFile(file string, line int) *sarifLocation {
	return locationFromFileWithColumn(file, line, 0)
}

func locationFromFileWithColumn(file string, line, column int) *sarifLocation {
	file = filepath.ToSlash(file)
	region := sarifRegion{StartLine: line}
	if column > 0 {
		region.StartColumn = column
	}
	return &sarifLocation{
		PhysicalLocation: sarifPhysicalLocation{
			ArtifactLocation: sarifArtifactLocation{URI: file},
			Region:           region,
		},
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexAny(s, "\n\r"); idx >= 0 {
		return strings.TrimSpace(s[:idx])
	}
	return s
}

func slugify(s string) string {
	s = strings.ToLower(s)
	var out strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			out.WriteRune(r)
		} else {
			out.WriteRune('_')
		}
	}
	res := out.String()
	res = strings.Trim(res, "_")
	res = regexp.MustCompile(`_+`).ReplaceAllString(res, "_")
	if res == "" {
		return "unknown"
	}
	return res
}

// ---------------------------------------------------------------------------
// SARIF data models (minimal subset required by the spec).
// ---------------------------------------------------------------------------

type sarifDocument struct {
	Version string     `json:"version"`
	Schema  string     `json:"$schema"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool        sarifTool         `json:"tool"`
	Results     []sarifResult     `json:"results"`
	Invocations []sarifInvocation `json:"invocations,omitempty"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	Version        string      `json:"version,omitempty"`
	InformationURI string      `json:"informationUri,omitempty"`
	Rules          []sarifRule `json:"rules,omitempty"`
}

type sarifRule struct {
	ID                   string                 `json:"id"`
	Name                 string                 `json:"name,omitempty"`
	ShortDescription     sarifMessage           `json:"shortDescription,omitempty"`
	FullDescription      sarifMessage           `json:"fullDescription,omitempty"`
	DefaultConfiguration sarifRuleConfiguration `json:"defaultConfiguration,omitempty"`
}

type sarifRuleConfiguration struct {
	Level string `json:"level"`
}

type sarifResult struct {
	RuleID     string          `json:"ruleId"`
	Level      string          `json:"level"`
	Message    sarifMessage    `json:"message"`
	Locations  []sarifLocation `json:"locations,omitempty"`
	Properties map[string]any  `json:"properties,omitempty"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           sarifRegion           `json:"region"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn,omitempty"`
}

type sarifInvocation struct {
	ExecutionSuccessful bool `json:"executionSuccessful"`
	ExitCode            int  `json:"exitCode"`
}
