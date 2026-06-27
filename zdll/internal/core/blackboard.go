package core

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

	// Resume only when target is empty (the caller's convention for resume).
	// New scans must start from a fresh blackboard even if a persisted workspace
	// already exists, otherwise round/task budgets and hypotheses leak across runs.
	if bm.store != nil && target == "" {
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
	bm.persistLocked()
	// CerebrumStarted is published by Engine.Run so that resume and new scans
	// emit it at the same lifecycle point.
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

	// Semantic deduplication: always check against existing hypotheses. Paraphrased
	// descriptions can bypass a Bloom pre-filter, so we run the full similarity
	// pipeline on every new hypothesis.
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

	h := NewHypothesisNode(description, confidence, parentID)
	bm.bb.Hypotheses[h.ID] = h
	bm.bloom.Add(description)
	bm.bb.touch()
	bm.persistLocked()
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
	changed := false
	for k, v := range updates {
		switch k {
		case "confidence":
			if f, ok := v.(float64); ok {
				if absFloat(h.Confidence-f) >= 0.001 {
					h.Confidence = f
					changed = true
				}
			}
		case "status":
			if s, ok := v.(string); ok {
				if h.Status != HypothesisStatus(s) {
					h.Status = HypothesisStatus(s)
					changed = true
				}
			}
		case "evidence":
			if s, ok := v.(string); ok {
				h.Evidence = append(h.Evidence, s)
				changed = true
			}
		}
	}
	if !changed {
		return nil
	}
	bm.bb.touch()
	bm.persistLocked()
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
	bm.persistLocked()
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
	bm.persistLocked()
	bm.publish(event.TaskUpdated, taskPayload(t))
	return nil
}

// MarkTaskIntegrated marks a task as having been processed by the result
// integrator. This prevents the same Drone output from being double-counted
// across multiple integration passes.
func (bm *BlackboardManager) MarkTaskIntegrated(id string) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	t, ok := bm.bb.Tasks[id]
	if !ok {
		return fmt.Errorf("task %s not found", id)
	}
	t.Integrated = true
	bm.bb.touch()
	bm.persistLocked()
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
	bm.persistLocked()
	bm.publish(event.FindingConfirmed, findingPayload(newFinding))
	bm.writeAuditNotesLocked()
	return newFinding.ID, nil
}

// RejectFinding marks a finding as rejected and records the reason. Rejected
// findings are excluded from reports and summary statistics by default.
func (bm *BlackboardManager) RejectFinding(id, reason string) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	for _, f := range bm.bb.Findings {
		if f.ID == id {
			f.Status = FindingStatusRejected
			f.RejectedReason = reason
			f.Severity = SeverityNone
			bm.bb.touch()
			bm.persistLocked()
			bm.publish(event.FindingRejectedByCritic, map[string]any{
				"finding_id": id,
				"reason":     reason,
			})
			return nil
		}
	}
	return fmt.Errorf("finding %s not found", id)
}

// GetFinding returns a finding by ID, or nil if not found.
func (bm *BlackboardManager) GetFinding(id string) *Finding {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	for _, f := range bm.bb.Findings {
		if f.ID == id {
			return f
		}
	}
	return nil
}

// SetFindingPrerequisites updates the exploit prerequisites of an existing
// finding. This is used by the exploit analyzer after a finding is created.
func (bm *BlackboardManager) SetFindingPrerequisites(id string, prereqs *ExploitPrerequisites) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	for _, f := range bm.bb.Findings {
		if f.ID == id {
			f.Prerequisites = prereqs
			bm.bb.touch()
			bm.persistLocked()
			return nil
		}
	}
	return fmt.Errorf("finding %s not found", id)
}

// WriteAuditNotes writes the current findings to a markdown audit notes file.
func (bm *BlackboardManager) WriteAuditNotes() {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	bm.writeAuditNotesLocked()
}

func (bm *BlackboardManager) writeAuditNotesLocked() {
	path := filepath.Join(bm.workDir, ".audit_notes.md")
	_ = os.WriteFile(path, []byte(formatAuditNotes(bm.bb)), 0o644)
}

func formatAuditNotes(bb *Blackboard) string {
	var b strings.Builder
	b.WriteString("# HIVE-MIND CONFIRMED FINDINGS\n\n")
	if len(bb.Findings) == 0 {
		b.WriteString("_No findings confirmed yet._\n")
		return b.String()
	}
	for _, f := range bb.SortedFindings() {
		fmt.Fprintf(&b, "### [LEAD: CONFIRMED] %s %s\n", strings.ToUpper(f.Severity), f.Title)
		fmt.Fprintf(&b, "- **Severity**: %s\n", strings.ToUpper(f.Severity))
		fmt.Fprintf(&b, "- **Description**: %s\n", f.Description)
		fmt.Fprintf(&b, "- **Evidence**: %s\n\n", f.Evidence)
	}
	return b.String()
}

func (bm *BlackboardManager) SetRound(round int) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	bm.bb.Round = round
	bm.bb.touch()
	bm.persistLocked()
}

func (bm *BlackboardManager) SetTarget(target string) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	bm.bb.Target = target
	bm.bb.touch()
	bm.persistLocked()
}

func (bm *BlackboardManager) SetActive(active bool) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	bm.bb.Active = active
	bm.bb.touch()
	bm.persistLocked()
}

func (bm *BlackboardManager) SetArtifactsDir(dir string) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	bm.bb.ArtifactsDir = dir
	bm.bb.touch()
	bm.persistLocked()
}

func (bm *BlackboardManager) Stats() map[string]int {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	return bm.bb.Stats()
}

func (bm *BlackboardManager) Snapshot() *Blackboard {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	return bm.bb.Clone()
}

func (bm *BlackboardManager) persistLocked() {
	if bm.store == nil {
		return
	}
	if err := bm.store.Save(bm.workDir, bm.bb); err != nil {
		bm.publish(event.PersistenceFailed, map[string]any{"error": err.Error()})
	}
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
// ActiveFindings returns non-rejected findings.
func (bb *Blackboard) ActiveFindings() []*Finding {
	out := make([]*Finding, 0, len(bb.Findings))
	for _, f := range bb.Findings {
		if f.Status != FindingStatusRejected {
			out = append(out, f)
		}
	}
	return out
}

// MergeFrom merges hypotheses and tasks from a child blackboard into this
// manager. This is used by the sector coordinator to bubble up sector-local
// hypotheses and tasks without going through the deduplication path, so that
// original IDs and task→hypothesis bindings are preserved.
func (bm *BlackboardManager) MergeFrom(child *Blackboard) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	for id, h := range child.Hypotheses {
		if existing, ok := bm.bb.Hypotheses[id]; ok {
			if h.Confidence > existing.Confidence {
				existing.Confidence = h.Confidence
			}
			if SeverityRank(string(h.Status)) > SeverityRank(string(existing.Status)) {
				existing.Status = h.Status
			}
			existing.Evidence = append(existing.Evidence, h.Evidence...)
		} else {
			bm.bb.Hypotheses[id] = h.Clone()
			bm.bloom.Add(h.Description)
		}
	}

	for id, t := range child.Tasks {
		if _, ok := bm.bb.Tasks[id]; !ok {
			bm.bb.Tasks[id] = t.Clone()
		}
	}

	bm.bb.touch()
	bm.persistLocked()
}

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
	bm.persistLocked()
}

// Report helpers.

func (bb *Blackboard) SortedFindings() []*Finding {
	active := bb.ActiveFindings()
	active = DeduplicateFindings(active)
	out := make([]*Finding, len(active))
	copy(out, active)
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
