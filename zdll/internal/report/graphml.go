package report

import (
	"fmt"
	"strings"
	"time"

	"zdll/internal/core"
)

// GraphMLRenderer generates a GraphML file for downstream graph tools.
type GraphMLRenderer struct{}

func (r *GraphMLRenderer) Name() string { return "graphml" }

func (r *GraphMLRenderer) Extension() string { return "graphml" }

func (r *GraphMLRenderer) Render(bb *core.Blackboard, target string, elapsed time.Duration) ([]byte, error) {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<graphml xmlns="http://graphml.graphdrawing.org/xmlns">` + "\n")
	b.WriteString(`  <key id="label" for="node" attr.name="label" attr.type="string"/>` + "\n")
	b.WriteString(`  <key id="color" for="node" attr.name="color" attr.type="string"/>` + "\n")
	b.WriteString(`  <key id="severity" for="node" attr.name="severity" attr.type="string"/>` + "\n")
	b.WriteString(`  <key id="edge_label" for="edge" attr.name="edge_label" attr.type="string"/>` + "\n")
	b.WriteString(`  <graph id="exploit_chain" edgedefault="directed">` + "\n")

	for _, h := range bb.Hypotheses {
		fmt.Fprintf(&b, "    <node id=\"%s\">\n", h.ID)
		fmt.Fprintf(&b, "      <data key=\"label\">%s</data>\n", escapeXML(h.Description))
		fmt.Fprintf(&b, "      <data key=\"color\">%s</data>\n", statusColor(h.Status))
		fmt.Fprintf(&b, "      <data key=\"severity\">%s</data>\n", h.Status)
		b.WriteString("    </node>\n")
	}

	for _, f := range bb.SortedFindings() {
		fmt.Fprintf(&b, "    <node id=\"%s\">\n", f.ID)
		fmt.Fprintf(&b, "      <data key=\"label\">%s</data>\n", escapeXML(f.Title))
		fmt.Fprintf(&b, "      <data key=\"color\">%s</data>\n", severityColor(f.Severity))
		fmt.Fprintf(&b, "      <data key=\"severity\">%s</data>\n", f.Severity)
		b.WriteString("    </node>\n")
		if f.HypothesisID != "" {
			fmt.Fprintf(&b, "    <edge source=\"%s\" target=\"%s\"/>\n", f.HypothesisID, f.ID)
		}
	}

	b.WriteString("  </graph>\n")
	b.WriteString("</graphml>\n")
	return []byte(b.String()), nil
}

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}
