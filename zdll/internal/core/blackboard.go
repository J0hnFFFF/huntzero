package core

import (
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"zdll/internal/event"
	"zdll/internal/eventbus"
)

// BlackboardManager wraps a Blackboard with thread-safe mutation and persistence.
type BlackboardManager struct {
	mu      sync.RWMutex
	bb      *Blackboard
	workDir string
	store   Store
	bus     eventbus.Bus
	bloom   *BloomDeduplicator
	nodeID  string
}

func NewBlackboardManager(workDir string, store Store, bus eventbus.Bus) *BlackboardManager {
	return &BlackboardManager{
		workDir: workDir,
		store:   store,
		bus:     bus,
		bloom:   NewBloomDeduplicator(10000, 7),
		nodeID:  "local",
	}
}

func (bm *BlackboardManager) Init(target string) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.store != nil {
		loaded, err := bm.store.Load(bm.workDir)
		if err == nil && loaded != nil {
			bm.bb = loaded
			// Re-populate bloom from existing hypotheses.
			for _, h := range bm.bb.Hypotheses {
				bm.bloom.Add(h.Description)
			}
			bm.publish(event.CerebrumResumed, map[string]any{
				"target": bm.bb.Target,
				"round":  bm.bb.Round,
			})
			return nil
		}
	}

	bm.bb = NewBlackboard(target)
	if err := bm.persistLocked(); err != nil {
		return err
	}
	bm.publish(event.CerebrumStarted, map[string]any{"target": target})
	return nil
}

func (bm *BlackboardManager) Blackboard() *Blackboard {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	return bm.bb
}

func (bm *BlackboardManager) WorkDir() string {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	return bm.workDir
}

func (bm *BlackboardManager) AddHypothesis(description string, confidence float64, parentID *string) (string, error) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if classifyHypothesisPolarity(description) == "negative" {
		return "", fmt.Errorf("negative polarity hypothesis rejected")
	}

	// Bloom pre-filter: if definitely not seen, it's new.
	if bm.bloom.MightContain(description) {
		if dupID, score := findSimilarHypothesis(bm.bb, description, 0.55); dupID != "" && score >= 0.55 {
			h := bm.bb.Hypotheses[dupID]
			// Merge confidence with small boost.
			h.Confidence = minFloat(1.0, maxFloat(h.Confidence, confidence)+0.02)
			if h.Status == HypothesisDiscarded && h.Confidence >= 0.4 {
				h.Status = HypothesisPending
			}
			h.Evidence = append(h.Evidence, fmt.Sprintf("[merged] similar hypothesis merged (score=%.2f)", score))
			bm.bb.touch()
			bm.persistLocked()
			bm.publish(event.HypothesisUpdated, hypothesisPayload(h))
			return dupID, nil
		}
	}

	h := NewHypothesisNode(description, confidence, parentID)
	bm.bb.Hypotheses[h.ID] = h
	bm.bloom.Add(description)
	bm.bb.touch()
	if err := bm.persistLocked(); err != nil {
		return "", err
	}
	bm.publish(event.HypothesisGenerated, hypothesisPayload(h))
	return h.ID, nil
}

func (bm *BlackboardManager) UpdateHypothesis(id string, updates map[string]any) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	h, ok := bm.bb.Hypotheses[id]
	if !ok {
		return fmt.Errorf("hypothesis %s not found", id)
	}
	for k, v := range updates {
		switch k {
		case "confidence":
			if f, ok := v.(float64); ok {
				h.Confidence = f
			}
		case "status":
			if s, ok := v.(string); ok {
				h.Status = HypothesisStatus(s)
			}
		case "evidence":
			if s, ok := v.(string); ok {
				h.Evidence = append(h.Evidence, s)
			}
		}
	}
	bm.bb.touch()
	if err := bm.persistLocked(); err != nil {
		return err
	}
	bm.publish(event.HypothesisUpdated, hypothesisPayload(h))
	return nil
}

func (bm *BlackboardManager) AddTask(hypothesisID, description, droneRole string) (string, error) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	h, ok := bm.bb.Hypotheses[hypothesisID]
	if !ok {
		return "", fmt.Errorf("hypothesis %s not found", hypothesisID)
	}

	// Task deduplication within same hypothesis + role.
	for _, tid := range h.Tasks {
		t, ok := bm.bb.Tasks[tid]
		if !ok {
			continue
		}
		if t.DroneRole == droneRole && isTaskDuplicate(t, description) {
			return tid, nil
		}
	}

	t := NewDroneTask(hypothesisID, description, droneRole)
	bm.bb.Tasks[t.ID] = t
	h.Tasks = append(h.Tasks, t.ID)
	if h.Status == HypothesisPending {
		h.Status = HypothesisActive
	}
	bm.bb.TotalTasks++
	bm.bb.touch()
	if err := bm.persistLocked(); err != nil {
		return "", err
	}
	bm.publish(event.TaskQueued, taskPayload(t))
	return t.ID, nil
}

func (bm *BlackboardManager) UpdateTask(id string, status TaskStatus, result, errStr *string) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	t, ok := bm.bb.Tasks[id]
	if !ok {
		return fmt.Errorf("task %s not found", id)
	}
	t.Status = status
	if result != nil {
		t.Result = result
	}
	if errStr != nil {
		t.Error = errStr
	}
	if status == TaskDone || status == TaskFailed || status == TaskTimeout {
		now := float64(time.Now().UnixMilli()) / 1000.0
		t.CompletedAt = &now
	}
	bm.bb.touch()
	if err := bm.persistLocked(); err != nil {
		return err
	}
	bm.publish(event.TaskUpdated, taskPayload(t))
	return nil
}

func (bm *BlackboardManager) AddFinding(hypothesisID, title, description, severity, evidence string, opts ...FindingOption) (string, error) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	newFinding := NewFinding(hypothesisID, title, description, severity, evidence)
	for _, opt := range opts {
		opt(newFinding)
	}

	// Finding deduplication.
	for _, f := range bm.bb.Findings {
		if isFindingDuplicate(f, newFinding) {
			f.Severity = mergeSeverity(f.Severity, newFinding.Severity)
			f.Evidence = mergeEvidence(f.Evidence, newFinding.Evidence)
			bm.bb.touch()
			bm.persistLocked()
			bm.publish(event.FindingConfirmed, findingPayload(f))
			return f.ID, nil
		}
	}

	bm.bb.Findings = append(bm.bb.Findings, newFinding)
	if h, ok := bm.bb.Hypotheses[hypothesisID]; ok {
		h.Status = HypothesisConfirmed
		h.Evidence = append(h.Evidence, fmt.Sprintf("[confirmed] finding %s", newFinding.ID))
	}
	bm.bb.touch()
	if err := bm.persistLocked(); err != nil {
		return "", err
	}
	bm.publish(event.FindingConfirmed, findingPayload(newFinding))
	return newFinding.ID, nil
}

func (bm *BlackboardManager) SetRound(round int) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	bm.bb.Round = round
	bm.bb.touch()
	return bm.persistLocked()
}

func (bm *BlackboardManager) SetActive(active bool) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	bm.bb.Active = active
	bm.bb.touch()
	return bm.persistLocked()
}

func (bm *BlackboardManager) Stats() map[string]int {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	return bm.bb.Stats()
}

func (bm *BlackboardManager) Snapshot() *Blackboard {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	return bm.bb
}

func (bm *BlackboardManager) persistLocked() error {
	if bm.store == nil {
		return nil
	}
	return bm.store.Save(bm.workDir, bm.bb)
}

func (bm *BlackboardManager) publish(typ string, data map[string]any) {
	if bm.bus == nil {
		return
	}
	if data == nil {
		data = map[string]any{}
	}
	data["type"] = typ
	data["timestamp"] = float64(time.Now().UnixMilli()) / 1000.0
	bm.bus.Publish(event.Event{Type: typ, Data: data})
}

func hypothesisPayload(h *HypothesisNode) map[string]any {
	return map[string]any{
		"id":             h.ID,
		"description":    h.Description,
		"confidence":     h.Confidence,
		"status":         string(h.Status),
		"parent_id":      h.ParentID,
		"tasks_count":    len(h.Tasks),
		"evidence_count": len(h.Evidence),
	}
}

func taskPayload(t *DroneTask) map[string]any {
	return map[string]any{
		"id":            t.ID,
		"hypothesis_id": t.HypothesisID,
		"description":   t.Description,
		"status":        string(t.Status),
		"drone_role":    t.DroneRole,
	}
}

func findingPayload(f *Finding) map[string]any {
	return map[string]any{
		"id":            f.ID,
		"hypothesis_id": f.HypothesisID,
		"title":         f.Title,
		"severity":      f.Severity,
		"finding_type":  f.FindingType,
		"evidence":      f.Evidence,
	}
}

// FindingOption configures a Finding.
type FindingOption func(*Finding)

func WithSectorID(sectorID string) FindingOption {
	return func(f *Finding) { f.SectorID = &sectorID }
}

func WithFindingType(ft FindingType) FindingOption {
	return func(f *Finding) { f.FindingType = ft }
}

func WithCVE(cve, pkg, version, fixed string) FindingOption {
	return func(f *Finding) {
		f.CVEID = cve
		f.PackageName = pkg
		f.PackageVersion = version
		f.FixedVersion = fixed
	}
}

func WithPrerequisites(p *ExploitPrerequisites) FindingOption {
	return func(f *Finding) { f.Prerequisites = p }
}

func WithLocation(loc *Location) FindingOption {
	return func(f *Finding) { f.Location = loc }
}

// FilterFindings removes findings that do not satisfy keep.
func (bm *BlackboardManager) FilterFindings(keep func(*Finding) bool) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	kept := bm.bb.Findings[:0]
	for _, f := range bm.bb.Findings {
		if keep(f) {
			kept = append(kept, f)
		}
	}
	bm.bb.Findings = kept
	bm.bb.touch()
	_ = bm.persistLocked()
}

// Report helpers.

func (bb *Blackboard) SortedFindings() []*Finding {
	out := make([]*Finding, len(bb.Findings))
	copy(out, bb.Findings)
	sort.Slice(out, func(i, j int) bool {
		return SeverityRank(out[i].Severity) > SeverityRank(out[j].Severity)
	})
	return out
}

func (bb *Blackboard) SummaryPayload() map[string]any {
	stats := bb.Stats()
	return map[string]any{
		"total_hypotheses":  stats["total_hypotheses"],
		"confirmed":         stats["confirmed"],
		"discarded":         stats["discarded"],
		"total_findings":    stats["total_findings"],
		"zero_day_findings": stats["zero_day_findings"],
		"dependency_vulns":  stats["dependency_vulns"],
		"tasks_executed":    stats["tasks_executed"],
	}
}

// WorkspaceDir returns the directory containing .blackboard.json.
func WorkspaceDir(workDir string) string {
	return workDir
}

// BlackboardPath returns the path to .blackboard.json.
func BlackboardPath(workDir string) string {
	return filepath.Join(workDir, ".blackboard.json")
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
