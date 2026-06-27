package report

import (
	"fmt"
	"strings"
	"time"

	"zdll/internal/core"
)

// DOTRenderer generates a Graphviz exploit-chain graph.
type DOTRenderer struct{}

func (r *DOTRenderer) Name() string { return "dot" }

func (r *DOTRenderer) Extension() string { return "dot" }

func (r *DOTRenderer) Render(bb *core.Blackboard, target string, elapsed time.Duration) ([]byte, error) {
	var b strings.Builder
	b.WriteString("digraph exploit_chain {\n")
	b.WriteString("  rankdir=LR;\n")
	b.WriteString("  node [shape=box, style=rounded, fontname=\"Helvetica\"];\n\n")

	for _, h := range bb.Hypotheses {
		color := statusColor(h.Status)
		label := fmt.Sprintf("%s\\n%s", h.ID, truncate(h.Description, 40))
		fmt.Fprintf(&b, "  \"%s\" [label=\"%s\", color=\"%s\"];\n", h.ID, label, color)
	}

	b.WriteString("\n")
	for _, f := range bb.SortedFindings() {
		color := severityColor(f.Severity)
		fmt.Fprintf(&b, "  \"%s\" [label=\"%s\\n%s\", shape=doublecircle, color=\"%s\"];\n", f.ID, f.ID, truncate(f.Title, 40), color)
		if f.HypothesisID != "" {
			fmt.Fprintf(&b, "  \"%s\" -> \"%s\";\n", f.HypothesisID, f.ID)
		}
	}

	b.WriteString("}\n")
	return []byte(b.String()), nil
}

func statusColor(s core.HypothesisStatus) string {
	switch s {
	case core.HypothesisConfirmed:
		return "#34c759"
	case core.HypothesisSuspected:
		return "#ffcc00"
	case core.HypothesisActive:
		return "#0a84ff"
	case core.HypothesisDiscarded:
		return "#8a8a9a"
	default:
		return "#cccccc"
	}
}

func severityColor(severity string) string {
	switch strings.ToLower(severity) {
	case core.SeverityCritical:
		return "#ff3b30"
	case core.SeverityHigh:
		return "#ff9500"
	case core.SeverityMedium:
		return "#ffcc00"
	case core.SeverityLow:
		return "#34c759"
	default:
		return "#cccccc"
	}
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}
