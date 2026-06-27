package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// BayesianEngine propagates confidence along hypothesis parent-child chains and
// learns the causal strength between linked hypotheses from historical outcomes.
// It mirrors the Python BayesianConfidenceEngine in engine/blackboard.py.
type BayesianEngine struct {
	mu             sync.Mutex
	hypotheses     map[string]*HypothesisNode
	strengthMatrix map[string]strengthEntry
	outcomes       []outcomeRecord
	recorded       map[string]struct{}
	strengthPath   string
}

type strengthEntry struct {
	Strength float64 `json:"strength"`
	N        int     `json:"n"`
	BaseRate float64 `json:"base_rate"`
}

type outcomeRecord struct {
	HID       string
	Confirmed bool
	ParentID  *string
	Idx       int
}

type bayesianFile struct {
	StrengthMatrix map[string]strengthEntry `json:"strength_matrix"`
	Meta           bayesianMeta             `json:"meta"`
}

type bayesianMeta struct {
	TotalOutcomes   int     `json:"total_outcomes"`
	DefaultStrength float64 `json:"default_strength"`
}

const (
	bayesianDefaultStrength = 0.85
	bayesianLaplaceAlpha    = 1.0
	bayesianMinSamples      = 3
)

// NewBayesianEngine creates an engine rooted at strengthPath. The learned
// strength matrix is loaded from <strengthPath>/.bayesian_strength.json if it
// exists.
func NewBayesianEngine(strengthPath string) *BayesianEngine {
	e := &BayesianEngine{
		strengthPath:   strengthPath,
		strengthMatrix: make(map[string]strengthEntry),
		recorded:       make(map[string]struct{}),
	}
	_ = e.load()
	return e
}

// RecordOutcome records a terminal outcome for a hypothesis and updates the
// learned strength of the parent->child link. Duplicate records are ignored.
func (e *BayesianEngine) RecordOutcome(h *HypothesisNode, confirmed bool) {
	if h == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.recorded[h.ID]; ok {
		return
	}
	e.recorded[h.ID] = struct{}{}
	rec := outcomeRecord{
		HID:       h.ID,
		Confirmed: confirmed,
		ParentID:  h.ParentID,
		Idx:       len(e.outcomes),
	}
	e.outcomes = append(e.outcomes, rec)
	if h.ParentID != nil {
		e.updateStrengthForPair(*h.ParentID, h.ID, confirmed)
	}
}

// PropagateAll computes posterior confidences by walking parent chains.
// confidence = 0.4 * own + 0.6 * parent_conf * learned_strength.
func (e *BayesianEngine) PropagateAll(hypotheses map[string]*HypothesisNode) map[string]float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	old := e.hypotheses
	defer func() { e.hypotheses = old }()
	e.hypotheses = hypotheses

	posteriors := make(map[string]float64, len(hypotheses))
	for id := range hypotheses {
		posteriors[id] = e.propagateSingle(id, make(map[string]struct{}))
	}
	return posteriors
}

// UpdateConfidences applies Bayesian posteriors to the blackboard and returns
// the number of hypotheses whose confidence changed meaningfully.
func (e *BayesianEngine) UpdateConfidences(bm *BlackboardManager) int {
	snap := bm.Snapshot()
	posteriors := e.PropagateAll(snap.Hypotheses)
	updated := 0
	for id, posterior := range posteriors {
		h, ok := snap.Hypotheses[id]
		if !ok {
			continue
		}
		if absFloat(h.Confidence-posterior) < 0.01 {
			continue
		}
		_ = bm.UpdateHypothesis(id, map[string]any{"confidence": clampFloat(posterior, 0, 1)})
		updated++
	}
	return updated
}

// Save persists the learned strength matrix to disk.
func (e *BayesianEngine) Save() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.strengthPath == "" {
		return nil
	}
	if err := os.MkdirAll(e.strengthPath, 0o755); err != nil {
		return fmt.Errorf("create bayesian path: %w", err)
	}
	path := filepath.Join(e.strengthPath, ".bayesian_strength.json")
	data := bayesianFile{
		StrengthMatrix: e.strengthMatrix,
		Meta: bayesianMeta{
			TotalOutcomes:   len(e.outcomes),
			DefaultStrength: bayesianDefaultStrength,
		},
	}
	buf, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, buf, 0o644)
}

func (e *BayesianEngine) load() error {
	if e.strengthPath == "" {
		return nil
	}
	path := filepath.Join(e.strengthPath, ".bayesian_strength.json")
	buf, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var data bayesianFile
	if err := json.Unmarshal(buf, &data); err != nil {
		return err
	}
	if data.StrengthMatrix != nil {
		e.strengthMatrix = data.StrengthMatrix
	}
	return nil
}

func (e *BayesianEngine) propagateSingle(id string, visited map[string]struct{}) float64 {
	if _, ok := visited[id]; ok {
		return 0.0
	}
	visited[id] = struct{}{}
	h, ok := e.hypotheses[id]
	if !ok || h == nil {
		return 0.0
	}
	if h.ParentID == nil {
		return h.Confidence
	}
	parentConf := e.propagateSingle(*h.ParentID, visited)
	strength := e.getStrength(*h.ParentID, id)
	propagated := parentConf * strength
	return 0.6*propagated + 0.4*h.Confidence
}

func (e *BayesianEngine) getStrength(parentID, childID string) float64 {
	key := pairKey(parentID, childID)
	entry, ok := e.strengthMatrix[key]
	if !ok || entry.N < bayesianMinSamples {
		return bayesianDefaultStrength
	}
	return entry.Strength
}

func (e *BayesianEngine) updateStrengthForPair(parentID, childID string, childConfirmed bool) {
	key := pairKey(parentID, childID)

	parentRecords := make([]outcomeRecord, 0, len(e.outcomes))
	childRecords := make([]outcomeRecord, 0, len(e.outcomes))
	for _, r := range e.outcomes {
		if r.HID == parentID {
			parentRecords = append(parentRecords, r)
		}
		if r.HID == childID {
			childRecords = append(childRecords, r)
		}
	}
	if len(parentRecords) == 0 || len(childRecords) == 0 {
		return
	}

	parentTime := 0
	for _, r := range parentRecords {
		if r.Idx > parentTime {
			parentTime = r.Idx
		}
	}
	childAfter := make([]outcomeRecord, 0, len(childRecords))
	for _, r := range childRecords {
		if r.Idx >= parentTime {
			childAfter = append(childAfter, r)
		}
	}

	nConfirmed := 0
	for _, r := range childAfter {
		if r.Confirmed {
			nConfirmed++
		}
	}
	nTotal := len(childAfter)
	nChildTotal := len(childRecords)
	nChildConfirmed := 0
	for _, r := range childRecords {
		if r.Confirmed {
			nChildConfirmed++
		}
	}

	baseRate := (float64(nChildConfirmed) + bayesianLaplaceAlpha) / (float64(nChildTotal) + 2*bayesianLaplaceAlpha)
	var strength float64
	if nTotal > 0 {
		conditional := (float64(nConfirmed) + bayesianLaplaceAlpha) / (float64(nTotal) + 2*bayesianLaplaceAlpha)
		if baseRate > 0 {
			strength = conditional / baseRate
		} else {
			strength = bayesianDefaultStrength
		}
	} else {
		strength = baseRate
	}
	if strength < 0 {
		strength = 0
	}
	if strength > 1 {
		strength = 1
	}

	existing := e.strengthMatrix[key]
	existing.N++
	existing.Strength = round4(strength)
	existing.BaseRate = round4(baseRate)
	e.strengthMatrix[key] = existing
}

func pairKey(parentID, childID string) string {
	return fmt.Sprintf("%s->%s", parentID, childID)
}

func round4(v float64) float64 {
	return float64(int64(v*10000+0.5)) / 10000
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
