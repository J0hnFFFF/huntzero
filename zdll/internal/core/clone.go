package core

// cloneString returns a pointer to a copy of s.
func cloneString(s *string) *string {
	if s == nil {
		return nil
	}
	v := *s
	return &v
}

// cloneFloat64 returns a pointer to a copy of f.
func cloneFloat64(f *float64) *float64 {
	if f == nil {
		return nil
	}
	v := *f
	return &v
}

// cloneStringSlice returns a shallow copy of the slice.
func cloneStringSlice(s []string) []string {
	if s == nil {
		return nil
	}
	out := make([]string, len(s))
	copy(out, s)
	return out
}

// Clone returns a deep copy of the prerequisites.
func (p *ExploitPrerequisites) Clone() *ExploitPrerequisites {
	if p == nil {
		return nil
	}
	return &ExploitPrerequisites{
		AuthRequired:     p.AuthRequired,
		NetworkAccess:    p.NetworkAccess,
		PrivilegeLevel:   p.PrivilegeLevel,
		EnvConfigs:       cloneStringSlice(p.EnvConfigs),
		CVEDependencies:  cloneStringSlice(p.CVEDependencies),
		ToolRequirements: cloneStringSlice(p.ToolRequirements),
		IsSatisfiable:    p.IsSatisfiable,
		UnmetConditions:  cloneStringSlice(p.UnmetConditions),
	}
}

// Clone returns a deep copy of the location.
func (l *Location) Clone() *Location {
	if l == nil {
		return nil
	}
	return &Location{File: l.File, Line: l.Line, Column: l.Column}
}

// Clone returns a deep copy of the finding.
func (f *Finding) Clone() *Finding {
	if f == nil {
		return nil
	}
	return &Finding{
		ID:             f.ID,
		HypothesisID:   f.HypothesisID,
		Title:          f.Title,
		Description:    f.Description,
		Severity:       f.Severity,
		Evidence:       f.Evidence,
		CreatedAt:      f.CreatedAt,
		Location:       f.Location.Clone(),
		SectorID:       cloneString(f.SectorID),
		PoCStatus:      f.PoCStatus,
		PoCOutput:      cloneString(f.PoCOutput),
		PoCVerifiedAt:  cloneFloat64(f.PoCVerifiedAt),
		Prerequisites:  f.Prerequisites.Clone(),
		Confidence:     f.Confidence,
		FindingType:    f.FindingType,
		CVEID:          f.CVEID,
		PackageName:    f.PackageName,
		PackageVersion: f.PackageVersion,
		FixedVersion:   f.FixedVersion,
		Status:         f.Status,
		RejectedReason: f.RejectedReason,
	}
}

// Clone returns a deep copy of the system model.
func (m *SystemModel) Clone() *SystemModel {
	if m == nil {
		return nil
	}
	boundaries := make([]Boundary, len(m.TrustBoundaries))
	copy(boundaries, m.TrustBoundaries)
	flows := make([]DataFlow, len(m.DataFlows))
	copy(flows, m.DataFlows)
	invariants := make([]Invariant, len(m.Invariants))
	copy(invariants, m.Invariants)
	zones := make([]string, len(m.OverconfidenceZones))
	copy(zones, m.OverconfidenceZones)
	anomalies := make([]string, len(m.Anomalies))
	copy(anomalies, m.Anomalies)
	assumptions := make([]Assumption, len(m.UntestedAssumptions))
	copy(assumptions, m.UntestedAssumptions)
	for i := range assumptions {
		assumptions[i].TestedBy = cloneStringSlice(assumptions[i].TestedBy)
	}
	return &SystemModel{
		TrustBoundaries:     boundaries,
		DataFlows:           flows,
		Invariants:          invariants,
		OverconfidenceZones: zones,
		Anomalies:           anomalies,
		UntestedAssumptions: assumptions,
	}
}

// Clone returns a deep copy of the hypothesis node.
func (h *HypothesisNode) Clone() *HypothesisNode {
	if h == nil {
		return nil
	}
	return &HypothesisNode{
		ID:                    h.ID,
		Description:           h.Description,
		Confidence:            h.Confidence,
		Status:                h.Status,
		Tasks:                 cloneStringSlice(h.Tasks),
		Evidence:              cloneStringSlice(h.Evidence),
		ParentID:              cloneString(h.ParentID),
		CreatedAt:             h.CreatedAt,
		Polarity:              h.Polarity,
		FalsificationAttempts: h.FalsificationAttempts,
	}
}

// Clone returns a deep copy of the drone task.
func (t *DroneTask) Clone() *DroneTask {
	if t == nil {
		return nil
	}
	return &DroneTask{
		ID:                 t.ID,
		HypothesisID:       t.HypothesisID,
		Description:        t.Description,
		Status:             t.Status,
		DroneRole:          t.DroneRole,
		Result:             cloneString(t.Result),
		Error:              cloneString(t.Error),
		CreatedAt:          t.CreatedAt,
		CompletedAt:        cloneFloat64(t.CompletedAt),
		Integrated:         t.Integrated,
		Falsification:      t.Falsification,
		TargetAssumptionID: t.TargetAssumptionID,
		ExplorationTarget:  t.ExplorationTarget,
	}
}

// Clone returns a deep copy of the blackboard, including all hypotheses,
// tasks, and findings. This matches the Python snapshot() semantics of
// returning an isolated copy that callers can safely read without holding
// the manager lock.
func (bb *Blackboard) Clone() *Blackboard {
	if bb == nil {
		return nil
	}
	clone := &Blackboard{
		Target:       bb.Target,
		Active:       bb.Active,
		Hypotheses:   make(map[string]*HypothesisNode, len(bb.Hypotheses)),
		Tasks:        make(map[string]*DroneTask, len(bb.Tasks)),
		Findings:     make([]*Finding, len(bb.Findings)),
		SystemModel:  bb.SystemModel.Clone(),
		Round:        bb.Round,
		TotalTasks:   bb.TotalTasks,
		CreatedAt:    bb.CreatedAt,
		UpdatedAt:    bb.UpdatedAt,
		Engine:       bb.Engine,
		ArtifactsDir: bb.ArtifactsDir,
	}
	for id, h := range bb.Hypotheses {
		clone.Hypotheses[id] = h.Clone()
	}
	for id, t := range bb.Tasks {
		clone.Tasks[id] = t.Clone()
	}
	for i, f := range bb.Findings {
		clone.Findings[i] = f.Clone()
	}
	return clone
}
