package report

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"time"

	"zdll/internal/core"
)

// MarkdownRenderer produces the human-readable report.
type MarkdownRenderer struct{}

func (r *MarkdownRenderer) Name() string { return "markdown" }

func (r *MarkdownRenderer) Extension() string { return "md" }

func (r *MarkdownRenderer) Render(bb *core.Blackboard, target string, elapsed time.Duration) ([]byte, error) {
	data := BuildRenderData(bb, target, elapsed)
	var b bytes.Buffer

	fmt.Fprintf(&b, "# 🔍 Security Analysis Report\n\n")
	fmt.Fprintf(&b, "| Field | Value |\n|-------|-------|\n")
	fmt.Fprintf(&b, "| Engine | %s |\n", data.Engine)
	fmt.Fprintf(&b, "| Target | `%s` |\n", data.Target)
	if data.ArtifactsDir != "" {
		fmt.Fprintf(&b, "| Artifacts | `%s` |\n", data.ArtifactsDir)
	}
	fmt.Fprintf(&b, "| Timestamp | %s |\n", data.Timestamp)
	fmt.Fprintf(&b, "| Duration | %.1fs |\n\n", data.ElapsedSeconds)

	fmt.Fprintf(&b, "## Summary\n\n")
	fmt.Fprintf(&b, "| Metric | Count |\n|--------|-------|\n")
	for _, k := range []string{"total_hypotheses", "confirmed", "discarded", "total_findings", "zero_day_findings", "dependency_vulns", "tasks_executed"} {
		fmt.Fprintf(&b, "| %s | %d |\n", strings.ReplaceAll(k, "_", " "), data.Summary[k])
	}

	fmt.Fprintf(&b, "\n## System Model\n\n")
	if data.SystemModel == nil || systemModelIsEmpty(data.SystemModel) {
		fmt.Fprintf(&b, "No system model recorded.\n\n")
	} else {
		b.Write(systemModelSection(data.SystemModel))
	}

	fmt.Fprintf(&b, "\n## Findings\n\n")
	if len(data.Findings) == 0 {
		fmt.Fprintf(&b, "No findings confirmed.\n\n")
	} else {
		for i, f := range data.Findings {
			fmt.Fprintf(&b, "### %s [%d] %s\n\n", severityEmoji(f.Severity), i+1, f.Title)
			fmt.Fprintf(&b, "- **Severity**: %s\n", strings.ToUpper(f.Severity))
			fmt.Fprintf(&b, "- **Hypothesis**: %s\n", f.HypothesisID)
			fmt.Fprintf(&b, "- **ID**: %s\n\n", f.ID)
			fmt.Fprintf(&b, "**Description:**\n\n%s\n\n", f.Description)
			fmt.Fprintf(&b, "**Evidence:**\n\n```\n%s\n```\n\n", sanitizeMarkdownFences(f.Evidence))
		}
	}

	fmt.Fprintf(&b, "## Hypotheses\n\n")
	if len(data.Hypotheses) == 0 {
		fmt.Fprintf(&b, "No hypotheses generated.\n\n")
	} else {
		fmt.Fprintf(&b, "| ID | Status | Confidence | Description |\n")
		fmt.Fprintf(&b, "|----|--------|------------|-------------|\n")
		for _, h := range data.Hypotheses {
			desc := h.Description
			if len(desc) > 80 {
				desc = desc[:77] + "..."
			}
			fmt.Fprintf(&b, "| %s | %s | %.2f | %s |\n", h.ID, h.Status, h.Confidence, desc)
		}
	}

	return b.Bytes(), nil
}

func systemModelIsEmpty(m *core.SystemModel) bool {
	return len(m.TrustBoundaries) == 0 && len(m.DataFlows) == 0 && len(m.Invariants) == 0 &&
		len(m.OverconfidenceZones) == 0 && len(m.Anomalies) == 0 && len(m.UntestedAssumptions) == 0
}

func systemModelSection(m *core.SystemModel) []byte {
	var b bytes.Buffer
	if len(m.TrustBoundaries) > 0 {
		fmt.Fprintf(&b, "### Trust Boundaries\n\n")
		for _, x := range m.TrustBoundaries {
			fmt.Fprintf(&b, "- **%s**: %s → %s  \n  %s\n", x.Name, x.UntrustedSide, x.TrustedSide, x.Description)
		}
		b.WriteString("\n")
	}
	if len(m.DataFlows) > 0 {
		fmt.Fprintf(&b, "### Data Flows\n\n")
		for _, x := range m.DataFlows {
			fmt.Fprintf(&b, "- **%s**: %s → %s  \n  %s\n", x.Name, x.Source, strings.Join(x.Sinks, ", "), x.Description)
		}
		b.WriteString("\n")
	}
	if len(m.Invariants) > 0 {
		fmt.Fprintf(&b, "### Invariants\n\n")
		for _, x := range m.Invariants {
			status := "untested"
			if x.Tested {
				status = "tested"
			}
			fmt.Fprintf(&b, "- [%s] %s\n", status, x.Statement)
		}
		b.WriteString("\n")
	}
	if len(m.OverconfidenceZones) > 0 {
		fmt.Fprintf(&b, "### Overconfidence Zones\n\n")
		for _, x := range m.OverconfidenceZones {
			fmt.Fprintf(&b, "- %s\n", x)
		}
		b.WriteString("\n")
	}
	if len(m.Anomalies) > 0 {
		fmt.Fprintf(&b, "### Anomalies\n\n")
		for _, x := range m.Anomalies {
			fmt.Fprintf(&b, "- %s\n", x)
		}
		b.WriteString("\n")
	}
	if len(m.UntestedAssumptions) > 0 {
		fmt.Fprintf(&b, "### Assumptions\n\n")
		fmt.Fprintf(&b, "| Status | Assumption | Verified by |\n")
		fmt.Fprintf(&b, "|--------|------------|-------------|\n")
		for _, a := range m.UntestedAssumptions {
			status := "untested"
			if a.Tested {
				status = a.Conclusion
				if status == "" {
					status = "tested"
				}
			}
			verifiedBy := "—"
			if len(a.TestedBy) > 0 {
				verifiedBy = strings.Join(a.TestedBy, ", ")
			}
			fmt.Fprintf(&b, "| %s | %s | %s |\n", status, a.Text, verifiedBy)
		}
		b.WriteString("\n")
	}
	return b.Bytes()
}

var fenceRe = regexp.MustCompile("`{3,}")

// sanitizeMarkdownFences replaces runs of three or more backticks with the
// same number of tildes so that evidence containing nested code fences does
// not break the report's outer code block.
func sanitizeMarkdownFences(s string) string {
	return fenceRe.ReplaceAllStringFunc(s, func(m string) string {
		return strings.Repeat("~", len(m))
	})
}

func severityEmoji(severity string) string {
	switch strings.ToLower(severity) {
	case core.SeverityCritical:
		return "🔴"
	case core.SeverityHigh:
		return "🟠"
	case core.SeverityMedium:
		return "🟡"
	case core.SeverityLow:
		return "🔵"
	default:
		return "⚪"
	}
}
