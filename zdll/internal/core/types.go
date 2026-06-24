package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// HypothesisStatus mirrors Python HypothesisStatus.
type HypothesisStatus string

const (
	HypothesisPending   HypothesisStatus = "pending"
	HypothesisActive    HypothesisStatus = "active"
	HypothesisSuspected HypothesisStatus = "suspected"
	HypothesisConfirmed HypothesisStatus = "confirmed"
	HypothesisDiscarded HypothesisStatus = "discarded"
)

// TaskStatus mirrors Python TaskStatus.
type TaskStatus string

const (
	TaskQueued  TaskStatus = "queued"
	TaskRunning TaskStatus = "running"
	TaskDone    TaskStatus = "done"
	TaskFailed  TaskStatus = "failed"
	TaskTimeout TaskStatus = "timeout"
)

// Scanner analyzes a target and returns dependency/semantic findings.
type Scanner interface {
	Name() string
	Scan(ctx context.Context, target string) ([]*Finding, error)
}

// Severity levels.
const (
	SeverityCritical = "critical"
	SeverityHigh     = "high"
	SeverityMedium   = "medium"
	SeverityLow      = "low"
	SeverityNone     = "none"
)

// PoCStatus mirrors Python poc_status.
type PoCStatus string

const (
	PoCPending         PoCStatus = "pending"
	PoCGenerated       PoCStatus = "generated"
	PoCVerifiedSuccess PoCStatus = "verified_success"
	PoCVerifiedFailed  PoCStatus = "verified_failed"
	PoCError           PoCStatus = "error"
)

// FindingType mirrors Python finding_type.
type FindingType string

const (
	FindingTypeZeroDay    FindingType = "zero_day"
	FindingTypeDependency FindingType = "dependency_vuln"
	FindingTypeSemantic   FindingType = "semantic_signal"
)

// ExploitPrerequisites mirrors Python ExploitPrerequisites.
type ExploitPrerequisites struct {
	AuthRequired     bool     `json:"auth_required"`
	NetworkAccess    string   `json:"network_access"`
	PrivilegeLevel   string   `json:"privilege_level"`
	EnvConfigs       []string `json:"env_configs"`
	CVEDependencies  []string `json:"cve_dependencies"`
	ToolRequirements []string `json:"tool_requirements"`
	IsSatisfiable    bool     `json:"is_satisfiable"`
	UnmetConditions  []string `json:"unmet_conditions"`
}

func NewExploitPrerequisites() *ExploitPrerequisites {
	return &ExploitPrerequisites{
		NetworkAccess:  "external",
		PrivilegeLevel: "none",
		IsSatisfiable:  true,
	}
}

// HypothesisNode mirrors Python HypothesisNode.
type HypothesisNode struct {
	ID          string           `json:"id"`
	Description string           `json:"description"`
	Confidence  float64          `json:"confidence"`
	Status      HypothesisStatus `json:"status"`
	Tasks       []string         `json:"tasks"`
	Evidence    []string         `json:"evidence"`
	ParentID    *string          `json:"parent_id,omitempty"`
	CreatedAt   float64          `json:"created_at"`
	Polarity    string           `json:"polarity"`
}

func NewHypothesisNode(description string, confidence float64, parentID *string) *HypothesisNode {
	return &HypothesisNode{
		ID:          generateID("H"),
		Description: description,
		Confidence:  confidence,
		Status:      HypothesisPending,
		Tasks:       []string{},
		Evidence:    []string{},
		ParentID:    parentID,
		CreatedAt:   float64(time.Now().UnixMilli()) / 1000.0,
		Polarity:    "positive",
	}
}

// DroneTask mirrors Python DroneTask.
type DroneTask struct {
	ID           string     `json:"id"`
	HypothesisID string     `json:"hypothesis_id"`
	Description  string     `json:"description"`
	Status       TaskStatus `json:"status"`
	DroneRole    string     `json:"drone_role"`
	Result       *string    `json:"result,omitempty"`
	Error        *string    `json:"error,omitempty"`
	CreatedAt    float64    `json:"created_at"`
	CompletedAt  *float64   `json:"completed_at,omitempty"`
}

func NewDroneTask(hypothesisID, description, role string) *DroneTask {
	return &DroneTask{
		ID:           generateID("T"),
		HypothesisID: hypothesisID,
		Description:  description,
		Status:       TaskQueued,
		DroneRole:    role,
		CreatedAt:    float64(time.Now().UnixMilli()) / 1000.0,
	}
}

// Location describes where a finding was detected.
type Location struct {
	File   string `json:"file"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
}

// Finding mirrors Python Finding.
type Finding struct {
	ID             string                `json:"id"`
	HypothesisID   string                `json:"hypothesis_id"`
	Title          string                `json:"title"`
	Description    string                `json:"description"`
	Severity       string                `json:"severity"`
	Evidence       string                `json:"evidence"`
	CreatedAt      float64               `json:"created_at"`
	Location       *Location             `json:"location,omitempty"`
	SectorID       *string               `json:"sector_id,omitempty"`
	PoCStatus      PoCStatus             `json:"poc_status"`
	PoCOutput      *string               `json:"poc_output,omitempty"`
	PoCVerifiedAt  *float64              `json:"poc_verified_at,omitempty"`
	Prerequisites  *ExploitPrerequisites `json:"prerequisites,omitempty"`
	FindingType    FindingType           `json:"finding_type"`
	CVEID          string                `json:"cve_id"`
	PackageName    string                `json:"package_name"`
	PackageVersion string                `json:"package_version"`
	FixedVersion   string                `json:"fixed_version"`
}

func NewFinding(hypothesisID, title, description, severity, evidence string) *Finding {
	return &Finding{
		ID:            generateID("F"),
		HypothesisID:  hypothesisID,
		Title:         title,
		Description:   description,
		Severity:      strings.ToLower(severity),
		Evidence:      evidence,
		CreatedAt:     float64(time.Now().UnixMilli()) / 1000.0,
		PoCStatus:     PoCPending,
		FindingType:   FindingTypeZeroDay,
		Prerequisites: NewExploitPrerequisites(),
	}
}

// SeverityRank returns numeric rank for sorting (higher = more severe).
func SeverityRank(severity string) int {
	switch strings.ToLower(severity) {
	case SeverityCritical:
		return 4
	case SeverityHigh:
		return 3
	case SeverityMedium:
		return 2
	case SeverityLow:
		return 1
	default:
		return 0
	}
}

// Blackboard is the single source of truth for an analysis session.
// Field names match the Python snapshot format so existing workspaces can be resumed.
type Blackboard struct {
	Target     string                     `json:"target"`
	Active     bool                       `json:"active"`
	Hypotheses map[string]*HypothesisNode `json:"hypotheses"`
	Tasks      map[string]*DroneTask      `json:"tasks"`
	Findings   []*Finding                 `json:"findings"`
	Round      int                        `json:"round"`
	TotalTasks int                        `json:"total_tasks"`
	CreatedAt  float64                    `json:"created_at"`
	UpdatedAt  float64                    `json:"updated_at"`
	Engine     string                     `json:"engine"`
}

func NewBlackboard(target string) *Blackboard {
	now := float64(time.Now().UnixMilli()) / 1000.0
	return &Blackboard{
		Target:     target,
		Active:     true,
		Hypotheses: make(map[string]*HypothesisNode),
		Tasks:      make(map[string]*DroneTask),
		Findings:   []*Finding{},
		CreatedAt:  now,
		UpdatedAt:  now,
		Engine:     "HIVE-MIND INTEL ENGINE V8.0",
	}
}

// Stats returns summary counts.
func (bb *Blackboard) Stats() map[string]int {
	confirmed := 0
	discarded := 0
	pending := 0
	suspected := 0
	for _, h := range bb.Hypotheses {
		switch h.Status {
		case HypothesisConfirmed:
			confirmed++
		case HypothesisDiscarded:
			discarded++
		case HypothesisPending:
			pending++
		case HypothesisSuspected:
			suspected++
		}
	}
	zeroDay := 0
	depVuln := 0
	for _, f := range bb.Findings {
		if f.FindingType == FindingTypeDependency {
			depVuln++
		} else {
			zeroDay++
		}
	}
	tasksExecuted := 0
	for _, t := range bb.Tasks {
		if t.Status == TaskDone {
			tasksExecuted++
		}
	}
	return map[string]int{
		"total_hypotheses":  len(bb.Hypotheses),
		"confirmed":         confirmed,
		"discarded":         discarded,
		"pending":           pending,
		"suspected":         suspected,
		"total_findings":    len(bb.Findings),
		"zero_day_findings": zeroDay,
		"dependency_vulns":  depVuln,
		"tasks_executed":    tasksExecuted,
		"total_tasks":       len(bb.Tasks),
	}
}

func (bb *Blackboard) touch() {
	bb.UpdatedAt = float64(time.Now().UnixMilli()) / 1000.0
}

func generateID(prefix string) string {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		// fallback to timestamp-based hex
		return fmt.Sprintf("%s-%06d", prefix, time.Now().UnixNano()%0xffffff)
	}
	return fmt.Sprintf("%s-%s", prefix, strings.ToUpper(hex.EncodeToString(b)))
}
