package event

import "time"

// Event is the unit of telemetry exchanged between core and consumers.
type Event struct {
	Type      string         `json:"type"`
	Data      map[string]any `json:"data"`
	Timestamp time.Time      `json:"timestamp"`
}

// Event type constants used by core and adapters.
const (
	CerebrumStarted        = "cerebrum_started"
	CerebrumResumed        = "cerebrum_resumed"
	CerebrumStopped        = "cerebrum_stopped"
	CerebrumComplete       = "cerebrum_complete"
	CerebrumRoundStarted   = "round_started"
	CerebrumPaused         = "cerebrum_paused"
	CerebrumError          = "cerebrum_error"
	CerebrumCriticReviewed = "cerebrum_critic_reviewed"
	CerebrumThought        = "cerebrum_thought"

	SystemModelUpdated = "system_model_updated"
	AssumptionTested   = "assumption_tested"

	HypothesisGenerated = "hypothesis_generated"
	HypothesisUpdated   = "hypothesis_updated"

	TaskQueued  = "task_queued"
	TaskUpdated = "task_updated"

	DroneLaunched  = "drone_launched"
	DroneCompleted = "drone_completed"
	DroneFailed    = "drone_failed"
	DroneTimeout   = "drone_timeout"

	FindingConfirmed        = "finding_confirmed"
	FindingRejectedByCritic = "finding_rejected_by_critic"
	FindingRetracted        = "finding_retracted"

	SectorAnalysisStarted   = "sector_analysis_started"
	SectorAnalysisCompleted = "sector_analysis_completed"
	SectorAnalysisFailed    = "sector_analysis_failed"

	CoordinatorStarted   = "coordinator_started"
	CoordinatorFinished  = "coordinator_finished"
	CrossSectorStarted   = "cross_sector_started"
	CrossSectorCompleted = "cross_sector_completed"
	CrossSectorFailed    = "cross_sector_failed"

	PersistenceFailed = "persistence_failed"

	Error = "error"
)
