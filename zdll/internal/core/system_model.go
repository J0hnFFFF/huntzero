package core

import (
	"fmt"
	"strings"
	"time"

	"zdll/internal/event"
)

const assumptionHolderID = "H-ASSUMPTIONS"

// UpdateSystemModel replaces the current system model and persists it.
func (bm *BlackboardManager) UpdateSystemModel(model *SystemModel) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if model == nil {
		return nil
	}
	ensureAssumptionHolder(bm.bb)
	if bm.bb.SystemModel == nil {
		bm.bb.SystemModel = NewSystemModel()
	}
	bm.bb.SystemModel.TrustBoundaries = model.TrustBoundaries
	bm.bb.SystemModel.DataFlows = model.DataFlows
	bm.bb.SystemModel.Invariants = model.Invariants
	bm.bb.SystemModel.OverconfidenceZones = model.OverconfidenceZones
	bm.bb.SystemModel.Anomalies = model.Anomalies

	// Merge assumptions: keep existing tested ones, add new untested ones, avoid
	// exact duplicates by text.
	existing := make(map[string]struct{}, len(bm.bb.SystemModel.UntestedAssumptions))
	for _, a := range bm.bb.SystemModel.UntestedAssumptions {
		existing[strings.ToLower(strings.TrimSpace(a.Text))] = struct{}{}
	}
	for _, a := range model.UntestedAssumptions {
		text := strings.TrimSpace(a.Text)
		if text == "" {
			continue
		}
		if _, ok := existing[strings.ToLower(text)]; ok {
			continue
		}
		existing[strings.ToLower(text)] = struct{}{}
		conclusion := normalizeAssumptionConclusion(a.Conclusion)
		if conclusion == "" {
			conclusion = AssumptionConclusionUntested
		}
		bm.bb.SystemModel.UntestedAssumptions = append(bm.bb.SystemModel.UntestedAssumptions, Assumption{
			ID:           generateID("A"),
			Text:         text,
			RoundCreated: a.RoundCreated,
			Tested:       false,
			Conclusion:   conclusion,
		})
	}

	bm.bb.touch()
	bm.persistLocked()
	bm.publish(event.SystemModelUpdated, map[string]any{
		"trust_boundaries": len(bm.bb.SystemModel.TrustBoundaries),
		"data_flows":       len(bm.bb.SystemModel.DataFlows),
		"invariants":       len(bm.bb.SystemModel.Invariants),
		"assumptions":      len(bm.bb.SystemModel.UntestedAssumptions),
	})
	return nil
}

// AddAssumption adds a single untested assumption to the system model.
func (bm *BlackboardManager) AddAssumption(text string, round int) (string, error) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("empty assumption")
	}
	if bm.bb.SystemModel == nil {
		bm.bb.SystemModel = NewSystemModel()
	}
	ensureAssumptionHolder(bm.bb)
	lower := strings.ToLower(text)
	for _, a := range bm.bb.SystemModel.UntestedAssumptions {
		if strings.ToLower(strings.TrimSpace(a.Text)) == lower {
			return a.ID, nil
		}
	}
	a := Assumption{
		ID:           generateID("A"),
		Text:         text,
		RoundCreated: round,
		Tested:       false,
		Conclusion:   AssumptionConclusionUntested,
	}
	bm.bb.SystemModel.UntestedAssumptions = append(bm.bb.SystemModel.UntestedAssumptions, a)
	bm.bb.touch()
	bm.persistLocked()
	return a.ID, nil
}

// MarkAssumptionTested marks an assumption as tested by a completed task.
func (bm *BlackboardManager) MarkAssumptionTested(id, taskID string) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.bb.SystemModel == nil {
		return fmt.Errorf("no system model")
	}
	for i := range bm.bb.SystemModel.UntestedAssumptions {
		if bm.bb.SystemModel.UntestedAssumptions[i].ID == id {
			bm.bb.SystemModel.UntestedAssumptions[i].Tested = true
			bm.bb.SystemModel.UntestedAssumptions[i].TestedBy = append(
				bm.bb.SystemModel.UntestedAssumptions[i].TestedBy,
				taskID,
			)
			if bm.bb.SystemModel.UntestedAssumptions[i].Conclusion == "" ||
				bm.bb.SystemModel.UntestedAssumptions[i].Conclusion == AssumptionConclusionUntested {
				bm.bb.SystemModel.UntestedAssumptions[i].Conclusion = AssumptionConclusionUnknown
			}
			bm.bb.touch()
			bm.persistLocked()
			bm.publish(event.AssumptionTested, map[string]any{
				"assumption_id": id,
				"task_id":       taskID,
				"conclusion":    bm.bb.SystemModel.UntestedAssumptions[i].Conclusion,
			})
			return nil
		}
	}
	return fmt.Errorf("assumption %s not found", id)
}

// SetAssumptionConclusion records the outcome of an assumption verification
// task. It marks the assumption as tested, stores the conclusion, and records
// the task that produced it.
func (bm *BlackboardManager) SetAssumptionConclusion(id, conclusion, taskID string) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.bb.SystemModel == nil {
		return fmt.Errorf("no system model")
	}
	conclusion = normalizeAssumptionConclusion(conclusion)
	if conclusion == "" {
		conclusion = AssumptionConclusionUnknown
	}
	for i := range bm.bb.SystemModel.UntestedAssumptions {
		if bm.bb.SystemModel.UntestedAssumptions[i].ID == id {
			bm.bb.SystemModel.UntestedAssumptions[i].Tested = true
			bm.bb.SystemModel.UntestedAssumptions[i].Conclusion = conclusion
			seen := false
			for _, tb := range bm.bb.SystemModel.UntestedAssumptions[i].TestedBy {
				if tb == taskID {
					seen = true
					break
				}
			}
			if !seen {
				bm.bb.SystemModel.UntestedAssumptions[i].TestedBy = append(
					bm.bb.SystemModel.UntestedAssumptions[i].TestedBy,
					taskID,
				)
			}
			bm.bb.touch()
			bm.persistLocked()
			bm.publish(event.AssumptionTested, map[string]any{
				"assumption_id": id,
				"task_id":       taskID,
				"conclusion":    conclusion,
			})
			return nil
		}
	}
	return fmt.Errorf("assumption %s not found", id)
}

func normalizeAssumptionConclusion(c string) string {
	c = strings.ToLower(strings.TrimSpace(c))
	switch c {
	case AssumptionConclusionValid, AssumptionConclusionViolated, AssumptionConclusionUnknown, AssumptionConclusionUntested:
		return c
	default:
		return ""
	}
}

// AssumptionHolderID returns the fixed hypothesis ID used for assumption
// verification tasks.
func (bm *BlackboardManager) AssumptionHolderID() string {
	return assumptionHolderID
}

// ensureAssumptionHolder creates a non-promotable meta hypothesis that owns
// assumption verification tasks, so that tasks can exist without being tied to
// a real vulnerability hypothesis.
func ensureAssumptionHolder(bb *Blackboard) {
	if bb.Hypotheses == nil {
		bb.Hypotheses = make(map[string]*HypothesisNode)
	}
	if _, ok := bb.Hypotheses[assumptionHolderID]; ok {
		return
	}
	now := float64(time.Now().UnixMilli()) / 1000.0
	bb.Hypotheses[assumptionHolderID] = &HypothesisNode{
		ID:          assumptionHolderID,
		Description: "Meta holder for assumption verification tasks",
		Confidence:  0.0,
		Status:      HypothesisPending,
		Tasks:       []string{},
		Evidence:    []string{},
		CreatedAt:   now,
		Polarity:    "positive",
	}
}
