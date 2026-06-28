package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"zdll/internal/config"
	"zdll/internal/core"
	"zdll/internal/event"
	"zdll/internal/eventbus"
	"zdll/internal/exploit"
	"zdll/internal/llm"
	"zdll/internal/report"
	"zdll/internal/scanner"

	"github.com/cloudwego/eino/components/model"
)

// App is the application service used by CLI.
type App struct {
	cfg             *config.Config
	bus             eventbus.Bus
	store           core.Store
	runner          llm.AgentRunner
	droneRunner     llm.AgentRunner
	exploitAnalyzer *exploit.Analyzer
	llmModel        model.BaseChatModel
	skillFS         core.SkillFS
	managers        map[string]*core.BlackboardManager
	// scanners optionally overrides the scanners used by the engine. When nil,
	// scanner.All(cfg) is used.
	scanners []core.Scanner
	// wg tracks background goroutines started by Scan.
	wg sync.WaitGroup
}

// ScanOptions controls optional scan behaviour.
type ScanOptions struct {
	NoReport     bool
	Output       string
	Format       string
	ChangedPaths []string
}

// ScanOption configures ScanOptions.
type ScanOption func(*ScanOptions)

// WithNoReport skips automatic report generation in the workspace.
func WithNoReport() ScanOption {
	return func(o *ScanOptions) { o.NoReport = true }
}

// WithOutput writes a report to the given path after completion.
func WithOutput(path string) ScanOption {
	return func(o *ScanOptions) { o.Output = path }
}

// WithFormat selects the report format for the output file.
func WithFormat(format string) ScanOption {
	return func(o *ScanOptions) {
		if format == "" {
			format = "markdown"
		}
		o.Format = format
	}
}

// WithChangedPaths scopes scanner and post-processing to the given changed files.
func WithChangedPaths(paths []string) ScanOption {
	return func(o *ScanOptions) { o.ChangedPaths = paths }
}

func New(cfg *config.Config, bus eventbus.Bus, s core.Store, runner llm.AgentRunner) *App {
	return &App{
		cfg:      cfg,
		bus:      bus,
		store:    s,
		runner:   runner,
		managers: make(map[string]*core.BlackboardManager),
	}
}

// WithDroneRunner sets a dedicated runner for Drone tasks. When unset, the
// Cerebrum runner is reused.
func (a *App) WithDroneRunner(r llm.AgentRunner) *App {
	a.droneRunner = r
	return a
}

// WithExploitAnalyzer sets a custom exploit analyzer. When unset, a default
// static-only analyzer is used.
func (a *App) WithExploitAnalyzer(analyzer *exploit.Analyzer) *App {
	a.exploitAnalyzer = analyzer
	return a
}

// WithLLMModel stores the base chat model so that optional LLM-backed
// subsystems (e.g. the exploit analyzer) can reuse it.
func (a *App) WithLLMModel(m model.BaseChatModel) *App {
	a.llmModel = m
	return a
}

// WithSkillFS sets the SkillFS used to load skill content. When unset, the
// engine falls back to a plain filesystem adapter based on cfg.Paths.Skills.
func (a *App) WithSkillFS(fs core.SkillFS) *App {
	a.skillFS = fs
	return a
}

// Scan starts a new analysis.
func (a *App) Scan(ctx context.Context, target string, resume bool, opts ...ScanOption) (*core.BlackboardManager, error) {
	options := &ScanOptions{}
	for _, opt := range opts {
		opt(options)
	}

	js := core.NewJSONStore(a.cfg.Paths.Workspace)
	workDir := js.WorkspacePath(target)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, fmt.Errorf("create workspace: %w", err)
	}

	targetDir, err := ResolveTarget(target, a.cfg.Paths.Workspace)
	if err != nil {
		return nil, err
	}

	bm := core.NewBlackboardManager(workDir, a.store, a.bus)
	if resume {
		if err := bm.Init(""); err != nil {
			return nil, fmt.Errorf("resume failed: %w", err)
		}
	} else {
		if err := bm.Init(target); err != nil {
			return nil, fmt.Errorf("init failed: %w", err)
		}
	}

	a.managers[target] = bm

	var coreScanners []core.Scanner
	if len(a.scanners) > 0 {
		coreScanners = a.scanners
	} else {
		scanners := scanner.All(a.cfg)
		for _, sc := range scanners {
			coreScanners = append(coreScanners, sc)
		}
	}
	engineCfg := &core.EngineConfig{
		Workers:      a.cfg.Analysis.Workers,
		MaxRounds:    a.cfg.Analysis.MaxRounds,
		MaxTasks:     a.cfg.Analysis.MaxTasks,
		MaxTime:      time.Duration(a.cfg.Analysis.MaxTime) * time.Second,
		Stagnation:   a.cfg.Analysis.Stagnation,
		AutoApprove:  a.cfg.Analysis.AutoApprove,
		Model:        a.cfg.LLM.Model,
		APIKey:       a.cfg.LLM.APIKey,
		BaseURL:      a.cfg.LLM.BaseURL,
		Scanners:     coreScanners,
		ArtifactsDir: filepath.Join(workDir, "artifacts"),
	}
	skillFS := a.skillFS
	if skillFS == nil {
		skillFS = core.NewPlainSkillFS(a.cfg.Paths.Skills)
	}
	engine := core.NewEngine(engineCfg, a.runner, skillFS, filepath.Dir(a.cfg.Paths.Skills), targetDir, a.bus)
	if a.droneRunner != nil {
		engine.WithDroneRunner(a.droneRunner)
	}
	if a.exploitAnalyzer != nil {
		engine.WithExploitAnalyzer(a.exploitAnalyzer)
	} else {
		engine.WithExploitAnalyzer(exploit.NewAnalyzer())
	}
	critic := core.NewCritic(a.runner, filepath.Dir(a.cfg.Paths.Skills), skillFS, a.bus)
	engine.WithCritic(critic)
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		start := time.Now()
		defer func() {
			if r := recover(); r != nil {
				stack := debug.Stack()
				msg := fmt.Sprintf("engine panic: %v\n%s", r, string(stack))
				// Publish to event bus and also write to stderr so it survives the CLI loop.
				a.publish(event.Error, map[string]any{"message": msg})
				fmt.Fprintln(os.Stderr, msg)
			}
		}()
		if len(options.ChangedPaths) > 0 {
			ctx = core.WithChangedPaths(ctx, options.ChangedPaths)
		}
		if err := engine.Run(ctx, bm); err != nil {
			a.publish(event.Error, map[string]any{"message": err.Error()})
		}
		elapsed := time.Since(start)
		if len(options.ChangedPaths) > 0 {
			a.filterFindingsByPaths(bm, options.ChangedPaths)
		}
		if !options.NoReport {
			a.exportReports(workDir, target, elapsed)
		}
		if options.Output != "" {
			a.exportReportTo(options.Output, target, elapsed, options.Format)
		}
		bm.SetActive(false)
		stats := bm.Stats()
		statsAny := make(map[string]any, len(stats))
		for k, v := range stats {
			statsAny[k] = v
		}
		a.publish(event.CerebrumComplete, statsAny)
	}()

	return bm, nil
}

// Resume aliases Scan with resume=true.
func (a *App) Resume(ctx context.Context, target string, opts ...ScanOption) (*core.BlackboardManager, error) {
	return a.Scan(ctx, target, true, opts...)
}

// Stop signals an analysis to stop by cancelling its context.
func (a *App) Stop(target string) error {
	// Context cancellation is handled by the caller; this is a placeholder.
	return nil
}

// Wait blocks until all background scans started by Scan have completed.
// Callers should cancel the context passed to Scan before calling Wait to
// ensure scans finish promptly.
func (a *App) Wait() {
	a.wg.Wait()
}

// Findings returns the current findings for a target.
func (a *App) Findings(target string) []*core.Finding {
	bm, ok := a.managers[target]
	if !ok {
		return nil
	}
	return bm.Snapshot().Findings
}

// Report generates a report artifact in memory.
func (a *App) Report(target string, renderer report.Renderer) ([]byte, error) {
	bm, ok := a.managers[target]
	if !ok {
		return nil, fmt.Errorf("no active analysis for %s", target)
	}
	return renderer.Render(bm.Snapshot(), target, 0)
}

// Config returns the current configuration.
func (a *App) Config() *config.Config {
	return a.cfg
}

func (a *App) publish(typ string, data map[string]any) {
	if a.bus == nil {
		return
	}
	a.bus.Publish(event.Event{Type: typ, Data: data, Timestamp: time.Now()})
}

func (a *App) exportReports(workDir, target string, elapsed time.Duration) {
	renderers := []report.Renderer{
		&report.MarkdownRenderer{},
		&report.JSONRenderer{},
		&report.SARIFRenderer{},
		&report.DOTRenderer{},
		&report.GraphMLRenderer{},
	}
	base := core.SanitizeFilename(filepath.Base(target))
	reportsDir := filepath.Join(workDir, "reports")
	for _, r := range renderers {
		data, err := r.Render(a.managers[target].Snapshot(), target, elapsed)
		if err != nil {
			msg := fmt.Sprintf("render %s report: %v", r.Name(), err)
			log.Printf("[report] %s", msg)
			a.publish(event.Error, map[string]any{"message": msg})
			continue
		}
		name := core.TimestampedReportName(base, r.Extension())
		if err := core.WriteReport(workDir, name, data); err != nil {
			msg := fmt.Sprintf("write %s report: %v", name, err)
			log.Printf("[report] %s", msg)
			a.publish(event.Error, map[string]any{"message": msg})
			continue
		}
		log.Printf("[report] wrote %s", filepath.Join(reportsDir, name))
	}
}

func (a *App) exportReportTo(path, target string, elapsed time.Duration, format string) {
	bm, ok := a.managers[target]
	if !ok {
		return
	}
	renderer, err := rendererByFormat(format)
	if err != nil {
		a.publish(event.Error, map[string]any{"message": fmt.Sprintf("output format: %v", err)})
		return
	}
	data, err := renderer.Render(bm.Snapshot(), target, elapsed)
	if err != nil {
		a.publish(event.Error, map[string]any{"message": fmt.Sprintf("render output: %v", err)})
		return
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		a.publish(event.Error, map[string]any{"message": fmt.Sprintf("write output: %v", err)})
	}
}

func rendererByFormat(format string) (report.Renderer, error) {
	switch strings.ToLower(format) {
	case "", "markdown", "md":
		return &report.MarkdownRenderer{}, nil
	case "json":
		return &report.JSONRenderer{}, nil
	case "sarif":
		return &report.SARIFRenderer{}, nil
	case "dot":
		return &report.DOTRenderer{}, nil
	case "graphml":
		return &report.GraphMLRenderer{}, nil
	default:
		return nil, fmt.Errorf("unknown report format: %s", format)
	}
}

func (a *App) filterFindingsByPaths(bm *core.BlackboardManager, paths []string) {
	set := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		set[filepath.ToSlash(filepath.Clean(p))] = struct{}{}
	}
	matches := func(file string) bool {
		if file == "" {
			return false
		}
		loc := filepath.ToSlash(filepath.Clean(file))
		if _, ok := set[loc]; ok {
			return true
		}
		// Allow relative locations to match absolute changed paths by suffix.
		if !filepath.IsAbs(file) {
			for p := range set {
				if strings.HasSuffix(p, "/"+loc) || (len(loc) > 0 && strings.HasSuffix(p, loc)) {
					return true
				}
			}
		}
		return false
	}
	bm.FilterFindings(func(f *core.Finding) bool {
		if f.Location != nil && matches(f.Location.File) {
			return true
		}
		// Fallback: parse a file path from evidence.
		if f.Evidence != "" {
			if loc := report.ExtractLocationFromEvidence(f.Evidence); loc != nil {
				return matches(loc.File)
			}
		}
		return false
	})
}
