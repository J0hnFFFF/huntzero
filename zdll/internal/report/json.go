package report

import (
	"encoding/json"
	"time"

	"zdll/internal/core"
)

// JSONRenderer produces the machine-readable report.
type JSONRenderer struct{}

func (r *JSONRenderer) Name() string { return "json" }

func (r *JSONRenderer) Extension() string { return "json" }

func (r *JSONRenderer) Render(bb *core.Blackboard, target string, elapsed time.Duration) ([]byte, error) {
	data := BuildRenderData(bb, target, elapsed)
	return json.MarshalIndent(data, "", "  ")
}
