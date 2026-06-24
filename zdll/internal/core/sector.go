package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"zdll/internal/event"
	"zdll/internal/eventbus"
	"zdll/internal/llm"
)

const (
	largeProjectFileThreshold = 5000
	largeProjectSizeThreshold = 50 * 1024 * 1024
	sectorIdealMinFiles       = 50
	sectorIdealMaxFiles       = 800
	maxSectors                = 8
)

// Sector represents a logical partition of a large project.
type Sector struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Path           string  `json:"path"`
	Description    string  `json:"description"`
	AttackSurface  string  `json:"attack_surface"`
	Priority       int     `json:"priority"`
	EstimatedFiles int     `json:"estimated_files"`
	Status         string  `json:"status"`
	FindingsCount  int     `json:"findings_count"`
	StartedAt      float64 `json:"started_at,omitempty"`
	CompletedAt    float64 `json:"completed_at,omitempty"`
	Error          string  `json:"error,omitempty"`
}

// SectorManager decides whether to partition a project and creates sectors.
type SectorManager struct {
	projectRoot string
	runner      llm.AgentRunner
	rootDir     string
	skillsDir   string
}

// NewSectorManager creates a heuristic-only sector manager.
func NewSectorManager(projectRoot string) *SectorManager {
	return &SectorManager{projectRoot: projectRoot}
}

// NewSectorManagerWithLLM creates a sector manager that can ask the LLM to
// decompose the project into logical attack-surface sectors.
func NewSectorManagerWithLLM(projectRoot string, runner llm.AgentRunner, rootDir, skillsDir string) *SectorManager {
	return &SectorManager{
		projectRoot: projectRoot,
		runner:      runner,
		rootDir:     rootDir,
		skillsDir:   skillsDir,
	}
}

// NeedsDecomposition returns true if the project is large enough to benefit from sectoring.
func (sm *SectorManager) NeedsDecomposition() (bool, error) {
	count, size, err := sm.scanScale()
	if err != nil {
		return false, err
	}
	return count > largeProjectFileThreshold || size > largeProjectSizeThreshold, nil
}

// Decompose creates sectors for the project. It first attempts an LLM-driven
// decomposition using document intelligence; if that fails, it falls back to a
// heuristic based on top-level directories.
func (sm *SectorManager) Decompose() ([]Sector, error) {
	if sm.runner != nil {
		sectors, err := sm.decomposeWithLLM()
		if err == nil && len(sectors) > 0 {
			return sectors, nil
		}
	}
	return sm.decomposeHeuristic()
}

func (sm *SectorManager) decomposeWithLLM() ([]Sector, error) {
	doc := NewDocIntel(sm.projectRoot)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	report, err := doc.Gather(ctx)
	if err != nil {
		return nil, err
	}

	dirs := sm.topLevelDirs()
	prompt := sm.buildDecompositionPrompt(report, dirs)
	cfg := llm.AgentConfig{
		Role:      "sector-planner",
		WorkDir:   sm.projectRoot,
		SkillsDir: sm.skillsDir,
		Thinking:  true,
	}
	if agentFile, err := llm.BuildAgentYAML(sm.rootDir, "sector-planner", ""); err == nil {
		cfg.AgentFile = agentFile
	}

	events, err := sm.runner.Run(ctx, cfg, prompt)
	if err != nil {
		return nil, err
	}
	var text string
	for ev := range events {
		if ev.Type == "text" {
			text += ev.Content
		}
		if ev.Type == "error" {
			return nil, fmt.Errorf("sector agent error: %s", ev.Content)
		}
	}

	jsonStr := extractJSON(text)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON in sector response")
	}

	var parsed []struct {
		ID            string   `json:"id"`
		Name          string   `json:"name"`
		Paths         []string `json:"paths"`
		Description   string   `json:"description"`
		AttackSurface string   `json:"attack_surface"`
		Priority      int      `json:"priority"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return nil, err
	}

	var sectors []Sector
	for _, p := range parsed {
		if len(p.Paths) == 0 {
			continue
		}
		id := p.ID
		if id == "" {
			id = fmt.Sprintf("S-%s", strings.ToUpper(sanitizeTaskID(p.Name)))
		}
		// Resolve sector path relative to project root; prefer the first existing path.
		var relPath string
		for _, candidate := range p.Paths {
			candidate = filepath.Clean(candidate)
			if !filepath.IsAbs(candidate) {
				if _, err := os.Stat(filepath.Join(sm.projectRoot, candidate)); err == nil {
					relPath = candidate
					break
				}
			}
		}
		if relPath == "" {
			continue
		}
		count, _, _ := sm.dirScale(filepath.Join(sm.projectRoot, relPath))
		sectors = append(sectors, Sector{
			ID:             id,
			Name:           p.Name,
			Path:           relPath,
			Description:    p.Description,
			AttackSurface:  p.AttackSurface,
			Priority:       p.Priority,
			EstimatedFiles: count,
			Status:         "pending",
		})
		if len(sectors) >= maxSectors {
			break
		}
	}

	if len(sectors) == 0 {
		return nil, fmt.Errorf("LLM decomposition produced no valid sectors")
	}
	sort.Slice(sectors, func(i, j int) bool { return sectors[i].Priority < sectors[j].Priority })
	return sectors, nil
}

func (sm *SectorManager) buildDecompositionPrompt(report *DocIntelReport, dirs []string) string {
	var b strings.Builder
	b.WriteString("You are a security audit planner. Split the target project into logical sectors for parallel analysis.\n\n")
	b.WriteString("Project context:\n")
	b.WriteString(report.SummaryMarkdown())
	b.WriteString("\n\nTop-level directories/files:\n")
	for _, d := range dirs {
		b.WriteString("- ")
		b.WriteString(d)
		b.WriteString("\n")
	}
	b.WriteString("\nInstructions:\n")
	fmt.Fprintf(&b, "- Produce at most %d sectors.\n", maxSectors)
	b.WriteString("- Each sector should group related attack surface (e.g., web API, authentication, data layer, CLI, core library).\n")
	b.WriteString("- Give each sector a short id, name, relative paths it covers, description, attack_surface, and priority (0 = highest).\n")
	b.WriteString("- Return strictly valid JSON array.\n")
	b.WriteString(`[{"id":"S-AUTH","name":"Authentication","paths":["auth","login.go"],"description":"...","attack_surface":"login/session endpoints","priority":0}]` + "\n")
	return b.String()
}

func (sm *SectorManager) topLevelDirs() []string {
	entries, err := os.ReadDir(sm.projectRoot)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
			continue
		}
		dirs = append(dirs, name)
	}
	sort.Strings(dirs)
	return dirs
}

func (sm *SectorManager) decomposeHeuristic() ([]Sector, error) {
	entries, err := os.ReadDir(sm.projectRoot)
	if err != nil {
		return nil, err
	}

	var sectors []Sector
	idx := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), ".") || e.Name() == "node_modules" || e.Name() == "vendor" {
			continue
		}
		path := filepath.Join(sm.projectRoot, e.Name())
		count, _, err := sm.dirScale(path)
		if err != nil || count < sectorIdealMinFiles {
			continue
		}

		sector := Sector{
			ID:             fmt.Sprintf("S-%s", strings.ToUpper(sanitizeTaskID(e.Name()))),
			Name:           e.Name(),
			Path:           e.Name(),
			Description:    fmt.Sprintf("Code under %s/", e.Name()),
			AttackSurface:  "internal API surface",
			Priority:       idx,
			EstimatedFiles: count,
			Status:         "pending",
		}
		sectors = append(sectors, sector)
		idx++
		if len(sectors) >= maxSectors {
			break
		}
	}

	if len(sectors) == 0 {
		// Fallback: treat whole project as one sector.
		count, _, _ := sm.scanScale()
		sectors = append(sectors, Sector{
			ID:             "S-ROOT",
			Name:           "root",
			Path:           ".",
			Description:    "Whole project",
			AttackSurface:  "all entry points",
			Priority:       0,
			EstimatedFiles: count,
			Status:         "pending",
		})
	}

	// Sort by priority (lower number = higher priority).
	sort.Slice(sectors, func(i, j int) bool { return sectors[i].Priority < sectors[j].Priority })
	return sectors, nil
}

// SectorWorkDir returns the workspace subdirectory for a sector.
func (sm *SectorManager) SectorWorkDir(parentWorkDir string, sector Sector) string {
	return filepath.Join(parentWorkDir, "sectors", sector.ID)
}

func (sm *SectorManager) scanScale() (files int, size int64, err error) {
	return sm.dirScale(sm.projectRoot)
}

func (sm *SectorManager) dirScale(root string) (files int, size int64, err error) {
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		files++
		size += info.Size()
		return nil
	})
	return
}

// Coordinator runs the engine over each sector and merges findings into the parent blackboard.
type Coordinator struct {
	engineFactory func(targetDir string) *Engine
	bus           eventbus.Bus
}

func NewCoordinator(factory func(targetDir string) *Engine, bus eventbus.Bus) *Coordinator {
	return &Coordinator{engineFactory: factory, bus: bus}
}

// Run executes analysis across all sectors with bounded parallelism.
func (c *Coordinator) Run(ctx context.Context, parent *BlackboardManager, sectors []Sector, parentWorkDir string, sm *SectorManager) error {
	maxConcurrent := 3
	if len(sectors) < maxConcurrent {
		maxConcurrent = len(sectors)
	}
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup

	for i := range sectors {
		wg.Add(1)
		sem <- struct{}{}
		go func(sector *Sector) {
			defer wg.Done()
			defer func() { <-sem }()
			c.runSector(ctx, parent, sector, parentWorkDir, sm)
		}(&sectors[i])
	}
	wg.Wait()

	c.publish(event.CrossSectorStarted, map[string]any{"sectors": len(sectors)})
	c.crossSectorAnalysis(ctx, parent, sectors)
	c.publish(event.CrossSectorCompleted, map[string]any{"findings": len(parent.Snapshot().Findings)})
	return nil
}

func (c *Coordinator) runSector(ctx context.Context, parent *BlackboardManager, sector *Sector, parentWorkDir string, sm *SectorManager) {
	sector.Status = "running"
	sector.StartedAt = float64(time.Now().UnixMilli()) / 1000.0
	c.publish(event.SectorAnalysisStarted, map[string]any{
		"sector_id":   sector.ID,
		"sector_name": sector.Name,
	})

	sectorWorkDir := sm.SectorWorkDir(parentWorkDir, *sector)
	_ = os.MkdirAll(sectorWorkDir, 0o755)

	child := NewBlackboardManager(sectorWorkDir, parent.store, parent.bus)
	_ = child.Init(fmt.Sprintf("%s#%s", parent.Snapshot().Target, sector.Path))

	targetDir := filepath.Join(sm.projectRoot, sector.Path)
	engine := c.engineFactory(targetDir)
	if err := engine.Run(ctx, child); err != nil {
		sector.Status = "failed"
		sector.Error = err.Error()
		c.publish(event.SectorAnalysisFailed, map[string]any{"sector_id": sector.ID, "error": err.Error()})
		return
	}

	// Bubble findings up to parent with sector_id.
	for _, f := range child.Snapshot().Findings {
		_, _ = parent.AddFinding(f.HypothesisID, f.Title, f.Description, f.Severity, f.Evidence, WithSectorID(sector.ID))
	}

	sector.Status = "done"
	sector.CompletedAt = float64(time.Now().UnixMilli()) / 1000.0
	sector.FindingsCount = len(child.Snapshot().Findings)
	c.publish(event.SectorAnalysisCompleted, map[string]any{
		"sector_id":   sector.ID,
		"findings":    sector.FindingsCount,
	})
}

func (c *Coordinator) crossSectorAnalysis(ctx context.Context, parent *BlackboardManager, sectors []Sector) {
	if len(parent.Snapshot().Findings) < 2 {
		return
	}
	// Placeholder for LLM-driven cross-sector exploit chain analysis.
	// The parent blackboard already contains all sector findings, so a future
	// implementation can prompt an LLM to identify multi-stage attack chains.
}

func (c *Coordinator) publish(typ string, data map[string]any) {
	if c.bus == nil {
		return
	}
	c.bus.Publish(event.Event{Type: typ, Data: data, Timestamp: time.Now()})
}
