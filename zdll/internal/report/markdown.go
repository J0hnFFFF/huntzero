package report

import (
	"bytes"
	"fmt"
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
	fmt.Fprintf(&b, "| Timestamp | %s |\n", data.Timestamp)
	fmt.Fprintf(&b, "| Duration | %.1fs |\n\n", data.ElapsedSeconds)

	fmt.Fprintf(&b, "## Summary\n\n")
	fmt.Fprintf(&b, "| Metric | Count |\n|--------|-------|\n")
	for _, k := range []string{"total_hypotheses", "confirmed", "discarded", "total_findings", "zero_day_findings", "dependency_vulns", "tasks_executed"} {
		fmt.Fprintf(&b, "| %s | %d |\n", strings.ReplaceAll(k, "_", " "), data.Summary[k])
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
			fmt.Fprintf(&b, "**Evidence:**\n\n```\n%s\n```\n\n", f.Evidence)
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
