package report

import (
	"time"

	"zdll/internal/core"
)

// Renderer turns a Blackboard into a report artifact.
type Renderer interface {
	Name() string
	Render(bb *core.Blackboard, target string, elapsed time.Duration) ([]byte, error)
	Extension() string
}

// RenderData is the public report shape compatible with the Python reports.
type RenderData struct {
	Engine                   string            `json:"engine"`
	Target                   string            `json:"target"`
	Timestamp                string            `json:"timestamp"`
	ElapsedSeconds           float64           `json:"elapsed_seconds"`
	Summary                  map[string]any    `json:"summary"`
	Findings                 []*core.Finding   `json:"findings"`
	ZeroDayFindings          []*core.Finding   `json:"zero_day_findings"`
	DependencyVulnerabilities []*core.Finding  `json:"dependency_vulnerabilities"`
	Hypotheses               []*core.HypothesisNode `json:"hypotheses"`
}

func BuildRenderData(bb *core.Blackboard, target string, elapsed time.Duration) *RenderData {
	findings := bb.SortedFindings()
	var zeroDay, depVulns []*core.Finding
	for _, f := range findings {
		if f.FindingType == core.FindingTypeDependency {
			depVulns = append(depVulns, f)
		} else {
			zeroDay = append(zeroDay, f)
		}
	}

	hypotheses := make([]*core.HypothesisNode, 0, len(bb.Hypotheses))
	for _, h := range bb.Hypotheses {
		hypotheses = append(hypotheses, h)
	}

	return &RenderData{
		Engine:                    bb.Engine,
		Target:                    target,
		Timestamp:                 time.Now().Format(time.RFC3339),
		ElapsedSeconds:            elapsed.Seconds(),
		Summary:                   bb.SummaryPayload(),
		Findings:                  findings,
		ZeroDayFindings:           zeroDay,
		DependencyVulnerabilities: depVulns,
		Hypotheses:                hypotheses,
	}
}
