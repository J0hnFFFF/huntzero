package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"zdll/internal/agent"
	"zdll/internal/app"
	"zdll/internal/config"

	"zdll/internal/core"
	"zdll/internal/event"
	"zdll/internal/eventbus"
	"zdll/internal/exploit"
	"zdll/internal/report"
	"zdll/internal/scanner"
	"zdll/internal/util"
	"zdll/internal/version"
)

type cliFlags struct {
	cfgFile          string
	workers          int
	maxRounds        int
	maxTasks         int
	maxTime          int
	resume           bool
	workDir          string
	reportFmt        string
	outputFile       string
	noReport         bool
	failOnSeverity   string
	baselineFile     string
	quiet            bool
	noColor          bool
	noEmoji          bool
	diffBase         string
	generateBaseline string
	summaryFormat    string
}

var flags cliFlags

var glyphMap = map[string]struct {
	emoji string
	ascii string
}{
	"brain":      {"🧠", "[H]"},
	"hypothesis": {"🧬", "[H]"},
	"drone":      {"🚁", "[D]"},
	"check":      {"✔", "[OK]"},
	"cross":      {"✖", "[X]"},
	"finding":    {"🟠", "[!]"},
	"complete":   {"✅", "[DONE]"},
}

func glyph(name string) string {
	if flags.noEmoji {
		return glyphMap[name].ascii
	}
	return glyphMap[name].emoji
}

// exitFunc allows tests to capture exit codes without terminating the process.
var exitFunc = os.Exit

func main() {
	rootCmd := newRootCmd()

	// Allow bare target usage: `zdll https://github.com/owner/repo` is treated as `zdll scan ...`.
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "scan", "resume", "ci", "report", "config", "list", "help", "completion", "--help", "-h", "--version", "-v":
			// explicit command / flag, do nothing
		default:
			rootCmd.SetArgs(append([]string{"scan"}, args...))
		}
	}

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:     "zdll",
		Short:   "zdll Hive-Mind security auditor",
		Long:    "A Go rewrite of the kimiSec Hive-Mind zero-day discovery engine.",
		Version: version.CLI,
	}

	rootCmd.PersistentFlags().StringVar(&flags.cfgFile, "config", "", "config file path")

	scanCmd := &cobra.Command{
		Use:   "scan <target>",
		Short: "Start a new security analysis",
		Args:  cobra.ExactArgs(1),
		RunE:  runScan,
	}
	addScanFlags(scanCmd)

	resumeCmd := &cobra.Command{
		Use:   "resume <target>",
		Short: "Resume a previous analysis",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flags.resume = true
			return runScan(cmd, args)
		},
	}
	addScanFlags(resumeCmd)

	ciCmd := &cobra.Command{
		Use:   "ci <target>",
		Short: "Run an unattended CI security analysis",
		Args:  cobra.ExactArgs(1),
		RunE:  runCI,
	}
	addCIFlags(ciCmd)

	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
	}
	configInitCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize default configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := config.InitConfigDir()
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Config initialized at %s\n", path)
			return nil
		},
	}
	configValidateCmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate configuration and API key",
		RunE:  runConfigValidate,
	}
	configGetCmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Get a configuration value",
		Args:  cobra.ExactArgs(1),
		RunE:  runConfigGet,
	}
	configSetCmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Args:  cobra.ExactArgs(2),
		RunE:  runConfigSet,
	}
	configUnsetCmd := &cobra.Command{
		Use:   "unset <key>",
		Short: "Unset a configuration value (restore default)",
		Args:  cobra.ExactArgs(1),
		RunE:  runConfigUnset,
	}
	configCmd.AddCommand(configInitCmd, configValidateCmd, configGetCmd, configSetCmd, configUnsetCmd)

	reportCmd := &cobra.Command{
		Use:   "report <target>",
		Short: "Render a report from an existing workspace",
		Args:  cobra.ExactArgs(1),
		RunE:  runReport,
	}
	reportCmd.Flags().StringVarP(&flags.reportFmt, "format", "f", "markdown", "report format: markdown, json, sarif, dot, graphml")
	reportCmd.Flags().StringVarP(&flags.outputFile, "output", "o", "", "output file (default: stdout)")
	reportCmd.Flags().StringVarP(&flags.workDir, "work-dir", "d", "", "workspace directory (overrides config)")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List existing analysis workspaces",
		RunE:  runList,
	}

	rootCmd.AddCommand(scanCmd, resumeCmd, ciCmd, configCmd, reportCmd, listCmd)
	return rootCmd
}

func addScanFlags(cmd *cobra.Command) {
	cmd.Flags().IntVarP(&flags.workers, "workers", "w", 0, "max concurrent drones")
	cmd.Flags().IntVarP(&flags.maxRounds, "max-rounds", "R", 0, "max reasoning rounds")
	cmd.Flags().IntVarP(&flags.maxTasks, "max-tasks", "T", 0, "max drone tasks")
	cmd.Flags().IntVarP(&flags.maxTime, "max-time", "t", 0, "max wall-clock time in seconds")
	cmd.Flags().StringVarP(&flags.workDir, "work-dir", "d", "", "workspace directory (overrides config)")
	cmd.Flags().StringVarP(&flags.outputFile, "output", "o", "", "write report to this file")
	cmd.Flags().StringVarP(&flags.reportFmt, "format", "f", "markdown", "report format: markdown, json, sarif, dot, graphml")
	cmd.Flags().BoolVar(&flags.noReport, "no-report", false, "skip automatic report generation")
	cmd.Flags().BoolVar(&flags.noEmoji, "no-emoji", EmojiUnsupported || os.Getenv("ZDLL_NO_EMOJI") == "1" || strings.EqualFold(os.Getenv("ZDLL_NO_EMOJI"), "true"), "disable emoji glyphs")
	cmd.Flags().String("fail-on", "", "exit non-zero if findings at or above severity")
	cmd.Flags().StringVar(&flags.baselineFile, "baseline", "", "baseline file of finding keys")
	cmd.Flags().BoolVarP(&flags.quiet, "quiet", "q", false, "suppress non-essential output")
	cmd.Flags().BoolVar(&flags.noColor, "no-color", false, "disable colored output")
	cmd.Flags().StringVar(&flags.diffBase, "diff-base", "", "only scan files changed since this git ref")
	cmd.Flags().StringVar(&flags.generateBaseline, "generate-baseline", "", "write finding keys to this file after scanning")
}

func addCIFlags(cmd *cobra.Command) {
	cmd.Flags().IntVarP(&flags.workers, "workers", "w", 0, "max concurrent drones")
	cmd.Flags().IntVarP(&flags.maxRounds, "max-rounds", "R", 0, "max reasoning rounds")
	cmd.Flags().IntVarP(&flags.maxTasks, "max-tasks", "T", 0, "max drone tasks")
	cmd.Flags().IntVarP(&flags.maxTime, "max-time", "t", 0, "max wall-clock time in seconds")
	cmd.Flags().StringVarP(&flags.workDir, "work-dir", "d", "", "workspace directory (overrides config)")
	cmd.Flags().StringVarP(&flags.outputFile, "output", "o", "", "write report to this file")
	cmd.Flags().StringVarP(&flags.reportFmt, "format", "f", "sarif", "report format: markdown, json, sarif, dot, graphml")
	cmd.Flags().BoolVar(&flags.noReport, "no-report", false, "skip automatic report generation")
	cmd.Flags().BoolVar(&flags.noEmoji, "no-emoji", EmojiUnsupported || os.Getenv("ZDLL_NO_EMOJI") == "1" || strings.EqualFold(os.Getenv("ZDLL_NO_EMOJI"), "true"), "disable emoji glyphs")
	cmd.Flags().String("fail-on", "high", "exit non-zero if findings at or above severity")
	cmd.Flags().StringVar(&flags.baselineFile, "baseline", "", "baseline file of finding keys")
	cmd.Flags().BoolVarP(&flags.quiet, "quiet", "q", false, "suppress non-essential output")
	cmd.Flags().BoolVar(&flags.noColor, "no-color", false, "disable colored output")
	cmd.Flags().StringVar(&flags.diffBase, "diff-base", "", "only scan files changed since this git ref")
	cmd.Flags().StringVar(&flags.generateBaseline, "generate-baseline", "", "write finding keys to this file after scanning")
	cmd.Flags().StringVar(&flags.summaryFormat, "summary-format", "text", "ci summary format: text, json")
}

func runScan(cmd *cobra.Command, args []string) error {
	return runScanCommon(cmd, args, false)
}

func runCI(cmd *cobra.Command, args []string) error {
	return runScanCommon(cmd, args, true)
}

func runScanCommon(cmd *cobra.Command, args []string, ciMode bool) error {
	target := args[0]

	if flags.noColor || os.Getenv("NO_COLOR") != "" {
		color.NoColor = true
	}

	cfg, err := config.Load(flags.cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	applyScanFlagOverrides(cfg, ciMode)
	if cfg.LLM.APIKey == "" {
		return fmt.Errorf("LLM API key is required (set KIMI_API_KEY or ZDLL_LLM_API_KEY env var, or llm.api_key in config)")
	}

	chatModel, err := agent.NewChatModel(context.Background(), agent.ModelConfig{
		Provider: cfg.LLM.Provider,
		APIKey:   cfg.LLM.APIKey,
		BaseURL:  cfg.LLM.BaseURL,
		Model:    cfg.LLM.Model,
		Headers:  loadLLMHeadersFromEnv(),
	})
	if err != nil {
		return fmt.Errorf("create LLM model: %w", err)
	}

	provider := cfg.LLM.Provider
	if provider == "" || strings.EqualFold(provider, "kimi") {
		provider = agent.DetectProvider(cfg.LLM.BaseURL)
	}

	cerebrumRunner := agent.NewCerebrumRunner(chatModel).WithMemory(true)
	if strings.EqualFold(provider, "openai") {
		// Force JSON output for the strategic synthesis loop on OpenAI-compatible
		// endpoints (including Kimi Code /coding/v1). Anthropic endpoints do not
		// support response_format.
		cerebrumRunner = cerebrumRunner.WithExtraFields(map[string]any{
			"response_format": map[string]any{"type": "json_object"},
		})
	}

	// Pre-warm a pool of tool-bound drone runners per role. This mirrors Python's
	// DroneSessionPool and avoids paying the model.WithTools cost on every task.
	dronePool := agent.NewRunnerPool(cfg.Paths.Workspace, chatModel, agent.DefaultDroneRoles, cfg.Analysis.Workers)
	if err := dronePool.Initialize(context.Background()); err != nil {
		log.Printf("warning: drone runner pool initialization failed: %v", err)
	}
	defer dronePool.Shutdown()
	droneRunner := agent.NewPooledRunner(dronePool)

	exploitAnalyzer := exploit.NewAnalyzer().
		WithLLM(exploit.NewModelClient(chatModel)).
		WithUseLLM(true)

	bus := eventbus.NewLocal()
	application := app.New(cfg, bus, core.NewJSONStore(cfg.Paths.Workspace), cerebrumRunner).
		WithDroneRunner(droneRunner).
		WithExploitAnalyzer(exploitAnalyzer).
		WithLLMModel(chatModel)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	setupSignalHandler(cancel)

	opts, changedPaths, err := buildScanOptions(cfg, target)
	if err != nil {
		return err
	}

	start := time.Now()
	logger := startEventLogger(bus, cfg, target)

	failOnSeverity, _ := cmd.Flags().GetString("fail-on")
	flags.failOnSeverity = failOnSeverity

	bm, err := application.Scan(ctx, target, flags.resume, opts...)
	if err != nil {
		return err
	}

	if !waitForScanComplete(ctx, bus) {
		return nil
	}

	// Wait for the background export goroutine to finish writing reports before
	// printing final stats. Otherwise we may exit while reports are still being
	// rendered, causing the final message to show a reports directory that is
	// empty or missing today's files.
	application.Wait()

	return finalizeScan(cmd, cfg, bm, target, start, changedPaths, ciMode, logger)
}

func applyScanFlagOverrides(cfg *config.Config, ciMode bool) {
	if flags.workers > 0 {
		cfg.Analysis.Workers = flags.workers
	}
	if flags.maxRounds > 0 {
		cfg.Analysis.MaxRounds = flags.maxRounds
	}
	if flags.maxTasks > 0 {
		cfg.Analysis.MaxTasks = flags.maxTasks
	}
	if flags.maxTime > 0 {
		cfg.Analysis.MaxTime = flags.maxTime
	}
	if flags.workDir != "" {
		if !filepath.IsAbs(flags.workDir) {
			cwd, _ := os.Getwd()
			flags.workDir = filepath.Join(cwd, flags.workDir)
		}
		cfg.Paths.Workspace = flags.workDir
	}
	if ciMode {
		cfg.Analysis.AutoApprove = true
	}
}

func loadLLMHeadersFromEnv() map[string]string {
	headers := make(map[string]string)
	if ua := os.Getenv("ZDLL_LLM_USER_AGENT"); ua != "" {
		headers["User-Agent"] = ua
	}
	if raw := os.Getenv("ZDLL_LLM_HEADERS"); raw != "" {
		for _, part := range strings.Split(raw, ";") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			kv := strings.SplitN(part, ":", 2)
			if len(kv) == 2 {
				headers[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
			}
		}
	}
	if len(headers) == 0 {
		return nil
	}
	return headers
}

func setupSignalHandler(cancel context.CancelFunc) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nInterrupted. Stopping...")
		cancel()
	}()
}

func buildScanOptions(cfg *config.Config, target string) ([]app.ScanOption, []string, error) {
	var opts []app.ScanOption
	var changedPaths []string

	if flags.diffBase != "" {
		targetDir, err := app.ResolveTarget(target, cfg.Paths.Workspace)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve target for diff-base: %w", err)
		}
		changedPaths, err = scanner.GitChangedFiles(targetDir, flags.diffBase)
		if err != nil {
			return nil, nil, fmt.Errorf("diff-base %s: %w", flags.diffBase, err)
		}
		if !flags.quiet {
			fmt.Printf("Diff-base %s: %d changed file(s)\n", flags.diffBase, len(changedPaths))
		}
	}

	if len(changedPaths) > 0 {
		opts = append(opts, app.WithChangedPaths(changedPaths))
	}
	if flags.noReport {
		opts = append(opts, app.WithNoReport())
	}
	if flags.outputFile != "" {
		opts = append(opts, app.WithOutput(flags.outputFile), app.WithFormat(flags.reportFmt))
	}

	return opts, changedPaths, nil
}

func startEventLogger(bus eventbus.Bus, cfg *config.Config, target string) *cliLogger {
	if flags.quiet {
		return nil
	}
	logger := newCLILogger(bus.Subscribe(), cfg, target)
	go logger.run()
	printBanner(flags.resume, cfg)
	return logger
}

func waitForScanComplete(ctx context.Context, bus eventbus.Bus) bool {
	complete := make(chan struct{})
	go func() {
		ch := bus.Subscribe()
		defer bus.Unsubscribe(ch)
		for ev := range ch {
			if ev.Type == event.CerebrumComplete || ev.Type == event.CerebrumStopped {
				close(complete)
				return
			}
		}
	}()

	select {
	case <-complete:
		return true
	case <-ctx.Done():
		return false
	}
}

func finalizeScan(cmd *cobra.Command, cfg *config.Config, bm *core.BlackboardManager, target string, start time.Time, changedPaths []string, ciMode bool, logger *cliLogger) error {
	elapsed := time.Since(start)
	findings := bm.Snapshot().Findings
	_, newKeys, removedKeys, err := evaluateBaseline(findings, flags.baselineFile)
	if err != nil {
		return fmt.Errorf("load baseline: %w", err)
	}

	if ciMode {
		printCISummary(cmd.OutOrStdout(), target, findings, elapsed, changedPaths, flags.baselineFile, newKeys, removedKeys, flags.summaryFormat)
	} else if !flags.quiet {
		if logger != nil {
			logger.printFinal(cfg)
		} else {
			printFinalStats(bm.Stats(), cfg, bm, target, elapsed)
		}
	}

	if flags.failOnSeverity != "" {
		failed, count, err := evaluateSeverityPolicy(findings, flags.failOnSeverity)
		if err != nil {
			return err
		}
		if failed {
			if !flags.quiet {
				fmt.Printf("\nPolicy failure: %d finding(s) at or above %s severity\n", count, flags.failOnSeverity)
			}
			exitFunc(2)
		}
	}

	if len(newKeys) > 0 || len(removedKeys) > 0 {
		if !flags.quiet {
			fmt.Printf("\nBaseline drift detected: %d new, %d resolved\n", len(newKeys), len(removedKeys))
			sort.Strings(newKeys)
			for _, k := range newKeys {
				fmt.Printf("  + %s\n", k)
			}
			sort.Strings(removedKeys)
			for _, k := range removedKeys {
				fmt.Printf("  - %s\n", k)
			}
		}
		exitFunc(2)
	}

	if flags.generateBaseline != "" {
		if err := writeBaselineFile(flags.generateBaseline, findings); err != nil {
			return err
		}
	}

	return nil
}

func runReport(cmd *cobra.Command, args []string) error {
	target := args[0]

	cfg, err := config.Load(flags.cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if flags.workDir != "" {
		if !filepath.IsAbs(flags.workDir) {
			cwd, _ := os.Getwd()
			flags.workDir = filepath.Join(cwd, flags.workDir)
		}
		cfg.Paths.Workspace = flags.workDir
	}

	store := core.NewJSONStore(cfg.Paths.Workspace)
	wsPath := store.WorkspacePath(target)

	bm := core.NewBlackboardManager(wsPath, store, nil)
	if err := bm.Init(""); err != nil {
		return fmt.Errorf("load workspace for %s: %w", target, err)
	}

	var renderer report.Renderer
	switch strings.ToLower(flags.reportFmt) {
	case "markdown", "md":
		renderer = &report.MarkdownRenderer{}
	case "json":
		renderer = &report.JSONRenderer{}
	case "sarif":
		renderer = &report.SARIFRenderer{}
	case "dot":
		renderer = &report.DOTRenderer{}
	case "graphml":
		renderer = &report.GraphMLRenderer{}
	default:
		return fmt.Errorf("unknown report format: %s", flags.reportFmt)
	}

	data, err := renderer.Render(bm.Snapshot(), target, 0)
	if err != nil {
		return fmt.Errorf("render report: %w", err)
	}

	out := cmd.OutOrStdout()
	if flags.outputFile != "" {
		if err := os.WriteFile(flags.outputFile, data, 0o644); err != nil {
			return fmt.Errorf("write report: %w", err)
		}
		fmt.Fprintf(out, "Report written to %s\n", flags.outputFile)
	} else {
		out.Write(data)
	}
	return nil
}

func runConfigValidate(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(flags.cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Provider: %s\n", cfg.LLM.Provider)
	fmt.Fprintf(out, "Base URL: %s\n", cfg.LLM.BaseURL)
	fmt.Fprintf(out, "Model:    %s\n", cfg.LLM.Model)
	fmt.Fprintf(out, "Workspace: %s\n", cfg.Paths.Workspace)
	if cfg.LLM.APIKey == "" {
		return fmt.Errorf("LLM API key is not set (set KIMI_API_KEY or ZDLL_LLM_API_KEY env var, or llm.api_key in config)")
	}
	fmt.Fprintf(out, "API Key:  %s (set)\n", config.MaskSecret(cfg.LLM.APIKey))
	fmt.Fprintln(out, "Configuration is valid.")
	return nil
}

func runConfigUnset(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(flags.cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := unsetConfigValue(cfg, args[0]); err != nil {
		return err
	}
	path := flags.cfgFile
	if path == "" {
		path = config.DefaultConfigPath()
	}
	if err := config.Save(cfg, path); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Unset %s (restored default)\n", args[0])
	return nil
}

func runConfigGet(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(flags.cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	val, err := getConfigValue(cfg, args[0])
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), val)
	return nil
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(flags.cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := setConfigValue(cfg, args[0], args[1]); err != nil {
		return err
	}
	path := flags.cfgFile
	if path == "" {
		path = config.DefaultConfigPath()
	}
	if err := config.Save(cfg, path); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Set %s = %s\n", args[0], args[1])
	return nil
}

func getConfigValue(cfg *config.Config, key string) (string, error) {
	switch key {
	case "llm.provider":
		return cfg.LLM.Provider, nil
	case "llm.api_key":
		return cfg.LLM.APIKey, nil
	case "llm.base_url":
		return cfg.LLM.BaseURL, nil
	case "llm.model":
		return cfg.LLM.Model, nil
	case "llm.thinking":
		return strconv.FormatBool(cfg.LLM.Thinking), nil
	case "analysis.workers":
		return strconv.Itoa(cfg.Analysis.Workers), nil
	case "analysis.max_rounds":
		return strconv.Itoa(cfg.Analysis.MaxRounds), nil
	case "analysis.max_tasks":
		return strconv.Itoa(cfg.Analysis.MaxTasks), nil
	case "analysis.max_time":
		return strconv.Itoa(cfg.Analysis.MaxTime), nil
	case "analysis.stagnation":
		return strconv.Itoa(cfg.Analysis.Stagnation), nil
	case "analysis.auto_approve":
		return strconv.FormatBool(cfg.Analysis.AutoApprove), nil
	case "analysis.sector_threshold_files":
		return strconv.Itoa(cfg.Analysis.SectorThresholdFiles), nil
	case "paths.workspace":
		return cfg.Paths.Workspace, nil
	case "paths.skills":
		return cfg.Paths.Skills, nil
	case "paths.osv_scanner":
		return cfg.Paths.OSVScanner, nil
	default:
		return "", fmt.Errorf("unknown config key: %s", key)
	}
}

func setConfigValue(cfg *config.Config, key, value string) error {
	switch key {
	case "llm.provider":
		cfg.LLM.Provider = value
	case "llm.api_key":
		cfg.LLM.APIKey = value
	case "llm.base_url":
		cfg.LLM.BaseURL = value
	case "llm.model":
		cfg.LLM.Model = value
	case "llm.thinking":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid bool %q: %w", value, err)
		}
		cfg.LLM.Thinking = b
	case "analysis.workers", "analysis.max_rounds", "analysis.max_tasks", "analysis.max_time", "analysis.stagnation", "analysis.sector_threshold_files":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid int %q: %w", value, err)
		}
		switch key {
		case "analysis.workers":
			cfg.Analysis.Workers = n
		case "analysis.max_rounds":
			cfg.Analysis.MaxRounds = n
		case "analysis.max_tasks":
			cfg.Analysis.MaxTasks = n
		case "analysis.max_time":
			cfg.Analysis.MaxTime = n
		case "analysis.stagnation":
			cfg.Analysis.Stagnation = n
		case "analysis.sector_threshold_files":
			cfg.Analysis.SectorThresholdFiles = n
		}
	case "analysis.auto_approve":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid bool %q: %w", value, err)
		}
		cfg.Analysis.AutoApprove = b
	case "paths.workspace":
		cfg.Paths.Workspace = value
	case "paths.skills":
		cfg.Paths.Skills = value
	case "paths.osv_scanner":
		cfg.Paths.OSVScanner = value
	default:
		return fmt.Errorf("unknown config key: %s", key)
	}
	return nil
}

func unsetConfigValue(cfg *config.Config, key string) error {
	defaults := config.Default()
	switch key {
	case "llm.provider":
		cfg.LLM.Provider = defaults.LLM.Provider
	case "llm.api_key":
		cfg.LLM.APIKey = defaults.LLM.APIKey
	case "llm.base_url":
		cfg.LLM.BaseURL = defaults.LLM.BaseURL
	case "llm.model":
		cfg.LLM.Model = defaults.LLM.Model
	case "llm.thinking":
		cfg.LLM.Thinking = defaults.LLM.Thinking
	case "analysis.workers":
		cfg.Analysis.Workers = defaults.Analysis.Workers
	case "analysis.max_rounds":
		cfg.Analysis.MaxRounds = defaults.Analysis.MaxRounds
	case "analysis.max_tasks":
		cfg.Analysis.MaxTasks = defaults.Analysis.MaxTasks
	case "analysis.max_time":
		cfg.Analysis.MaxTime = defaults.Analysis.MaxTime
	case "analysis.stagnation":
		cfg.Analysis.Stagnation = defaults.Analysis.Stagnation
	case "analysis.auto_approve":
		cfg.Analysis.AutoApprove = defaults.Analysis.AutoApprove
	case "analysis.sector_threshold_files":
		cfg.Analysis.SectorThresholdFiles = defaults.Analysis.SectorThresholdFiles
	case "paths.workspace":
		cfg.Paths.Workspace = defaults.Paths.Workspace
	case "paths.skills":
		cfg.Paths.Skills = defaults.Paths.Skills
	case "paths.osv_scanner":
		cfg.Paths.OSVScanner = defaults.Paths.OSVScanner
	default:
		return fmt.Errorf("unknown config key: %s", key)
	}
	return nil
}

func runList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(flags.cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	store := core.NewJSONStore(cfg.Paths.Workspace)
	workspaces, err := store.List()
	if err != nil {
		return fmt.Errorf("list workspaces: %w", err)
	}
	out := cmd.OutOrStdout()
	if len(workspaces) == 0 {
		fmt.Fprintln(out, "No workspaces found.")
		return nil
	}
	fmt.Fprintf(out, "%-40s %-10s %-20s\n", "TARGET", "STATUS", "UPDATED")
	for _, ws := range workspaces {
		fmt.Fprintf(out, "%-40s %-10s %-20s\n", ws.Target, ws.Status, ws.UpdatedAt.Format("2006-01-02 15:04"))
	}
	return nil
}

func evaluateSeverityPolicy(findings []*core.Finding, threshold string) (bool, int, error) {
	if threshold == "" {
		return false, 0, nil
	}
	if _, err := report.ParseSeverity(threshold); err != nil {
		return false, 0, err
	}
	failed, count := report.SeverityAtOrAbove(findings, threshold)
	return failed, count, nil
}

func evaluateBaseline(findings []*core.Finding, baselineFile string) (baselineKeys, newKeys, removedKeys []string, err error) {
	if baselineFile == "" {
		return nil, nil, nil, nil
	}
	baselineKeys, err = report.LoadBaselineKeys(baselineFile)
	if err != nil {
		return nil, nil, nil, err
	}
	currentKeys := report.FindingKeys(findings)
	newKeys, removedKeys = report.CompareFindings(currentKeys, baselineKeys)
	return baselineKeys, newKeys, removedKeys, nil
}

func writeBaselineFile(path string, findings []*core.Finding) error {
	keys := report.FindingKeys(findings)
	data := []byte(strings.Join(keys, "\n"))
	if len(keys) > 0 {
		data = append(data, '\n')
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write baseline: %w", err)
	}
	if !flags.quiet {
		fmt.Printf("Baseline written to %s (%d key(s))\n", path, len(keys))
	}
	return nil
}

func printBanner(resume bool, cfg *config.Config) {
	mode := "UNATTENDED"
	if !cfg.Analysis.AutoApprove {
		mode = "INTERACTIVE"
	}
	action := "SCAN"
	if resume {
		action = "RESUME"
	}

	cyan := color.New(color.FgCyan, color.Bold).SprintFunc()
	dim := color.New(color.FgWhite, color.Faint).SprintFunc()
	white := color.New(color.FgWhite, color.Bold).SprintFunc()

	fmt.Println()
	fmt.Println(cyan("┌─────────────────────────────────────────┐"))
	fmt.Println(cyan("│") + white("  HIVE-MIND INTEL ENGINE  ") + cyan("│"))
	fmt.Println(cyan("│") + dim("  Autonomous · LLM-Dominant · Headless ") + cyan("│"))
	fmt.Println(cyan("└─────────────────────────────────────────┘"))
	fmt.Printf("  Mode:      %s\n", color.YellowString(mode))
	fmt.Printf("  Action:    %s\n", color.YellowString(action))
	fmt.Printf("  Workspace: %s\n", cfg.Paths.Workspace)
	fmt.Printf("  Workers:   %d\n", cfg.Analysis.Workers)
	fmt.Printf("  Model:     %s\n", cfg.LLM.Model)
	fmt.Println()
}

func printCISummary(out io.Writer, target string, findings []*core.Finding, elapsed time.Duration, changedPaths []string, baselineFile string, newKeys, removedKeys []string, format string) {
	sev := report.CountBySeverity(findings)
	types := report.CountByType(findings)

	failFailed, failCount := false, 0
	if flags.failOnSeverity != "" {
		failFailed, failCount = report.SeverityAtOrAbove(findings, flags.failOnSeverity)
	}
	baselineFailed := baselineFile != "" && (len(newKeys) > 0 || len(removedKeys) > 0)

	switch strings.ToLower(format) {
	case "json":
		data := map[string]any{
			"target":   target,
			"elapsed":  elapsed.Round(time.Second).String(),
			"findings": len(findings),
			"severity": sev,
			"types":    types,
			"changed":  len(changedPaths),
			"fail_on":  map[string]any{"threshold": flags.failOnSeverity, "failed": failFailed, "count": failCount},
			"baseline": map[string]any{"file": baselineFile, "failed": baselineFailed, "new": len(newKeys), "resolved": len(removedKeys)},
		}
		b, _ := json.MarshalIndent(data, "", "  ")
		fmt.Fprintln(out, string(b))
	default:
		fmt.Fprintln(out)
		fmt.Fprintln(out, "== zdll CI Summary ==")
		fmt.Fprintf(out, "Target:  %s\n", target)
		fmt.Fprintf(out, "Elapsed: %s\n", elapsed.Round(time.Second))
		if len(changedPaths) > 0 {
			fmt.Fprintf(out, "Diff-base: %d changed file(s)\n", len(changedPaths))
		}
		fmt.Fprintf(out, "Findings: total=%d critical=%d high=%d medium=%d low=%d none=%d\n",
			len(findings),
			sev[core.SeverityCritical],
			sev[core.SeverityHigh],
			sev[core.SeverityMedium],
			sev[core.SeverityLow],
			sev[core.SeverityNone],
		)
		fmt.Fprintf(out, "By type: dependency_vuln=%d zero_day=%d semantic_signal=%d\n",
			types[core.FindingTypeDependency],
			types[core.FindingTypeZeroDay],
			types[core.FindingTypeSemantic],
		)

		if flags.failOnSeverity != "" {
			status := "PASS"
			if failFailed {
				status = "FAIL"
			}
			fmt.Fprintf(out, "Fail-on threshold (%s): %s (%d finding(s))\n", flags.failOnSeverity, status, failCount)
		}

		if baselineFile != "" {
			status := "PASS"
			if baselineFailed {
				status = "FAIL"
			}
			fmt.Fprintf(out, "Baseline (%s): %s (%d new, %d resolved)\n", baselineFile, status, len(newKeys), len(removedKeys))
		}
	}
}

func printFinalStats(stats map[string]int, cfg *config.Config, bm *core.BlackboardManager, target string, elapsed time.Duration) {
	green := color.New(color.FgGreen, color.Bold).SprintFunc()
	fmt.Println()
	fmt.Printf("%s Done in %s\n", green(glyph("check")), elapsed.Round(time.Second))
	fmt.Printf("   Hypotheses: %d  Findings: %d  Tasks: %d\n", stats["total_hypotheses"], stats["total_findings"], stats["total_tasks"])
	if !flags.noReport {
		store := core.NewJSONStore(cfg.Paths.Workspace)
		fmt.Printf("   Reports: %s\n", filepath.Join(store.WorkspacePath(target), "reports"))
		fmt.Println(reportFilesSummary(cfg, target))
	}
	if artifactsDir := bm.Snapshot().ArtifactsDir; artifactsDir != "" {
		fmt.Printf("   PoCs/Artifacts: %s\n", artifactsDir)
	}
}

// reportFilesSummary returns a short list of report files actually present in
// the reports directory, or "(none yet)" if the directory is missing/empty.
func reportFilesSummary(cfg *config.Config, target string) string {
	store := core.NewJSONStore(cfg.Paths.Workspace)
	reportsDir := filepath.Join(store.WorkspacePath(target), "reports")
	entries, err := os.ReadDir(reportsDir)
	if err != nil || len(entries) == 0 {
		return "   (no report files found)"
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "   (no report files found)"
	}
	var sb strings.Builder
	for i, n := range names {
		if i >= 5 {
			fmt.Fprintf(&sb, "   ... and %d more\n", len(names)-i)
			break
		}
		fmt.Fprintf(&sb, "   - %s\n", n)
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

type cliLogger struct {
	ch           <-chan event.Event
	cfg          *config.Config
	target       string
	activeDrones int
	round        int
	findings     int
	hypotheses   int
	tasks        int
	start        time.Time
	lastStatus   map[string]string
	seenFindings map[string]struct{}
}

func newCLILogger(ch <-chan event.Event, cfg *config.Config, target string) *cliLogger {
	return &cliLogger{ch: ch, cfg: cfg, target: target, start: time.Now(), lastStatus: make(map[string]string), seenFindings: make(map[string]struct{})}
}

func (l *cliLogger) run() {
	red := color.New(color.FgRed).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	cyan := color.New(color.FgCyan).SprintFunc()
	purple := color.New(color.FgMagenta).SprintFunc()
	dim := color.New(color.FgWhite, color.Faint).SprintFunc()

	for ev := range l.ch {
		switch ev.Type {
		case event.CerebrumStarted:
			fmt.Println(cyan("─── Analysis started ───"))
		case event.CerebrumRoundStarted:
			l.round = util.IntValue(ev.Data["round"])
			l.printProgressLine()
		case event.HypothesisGenerated:
			l.hypotheses++
			fmt.Printf("%s %s %s\n", purple(glyph("hypothesis")), dim(fmt.Sprintf("%s [%.0f%%]", ev.Data["id"], util.FloatValue(ev.Data["confidence"])*100)), ev.Data["description"])
		case event.HypothesisUpdated:
			id := util.StringValue(ev.Data["id"])
			status := util.StringValue(ev.Data["status"])
			if l.lastStatus[id] == status {
				continue
			}
			l.lastStatus[id] = status
			fmt.Printf("%s %s → %s\n", purple(glyph("hypothesis")), id, dim(status))
		case event.TaskQueued:
			l.tasks++
			fmt.Printf("%s %s queued (%s)\n", dim(glyph("drone")), ev.Data["id"], ev.Data["drone_role"])
		case event.DroneLaunched:
			l.activeDrones++
			fmt.Printf("%s Drone %s launched (%s)\n", dim(glyph("drone")), ev.Data["task_id"], ev.Data["role"])
		case event.DroneCompleted, event.DroneFailed:
			if l.activeDrones > 0 {
				l.activeDrones--
			}
			if ev.Type == event.DroneFailed {
				fmt.Printf("%s Drone %s failed: %v\n", red(glyph("cross")), ev.Data["task_id"], ev.Data["error"])
			} else {
				fmt.Printf("%s Drone %s completed (%s)\n", green(glyph("check")), ev.Data["task_id"], ev.Data["role"])
			}
		case event.FindingConfirmed:
			key := util.StringValue(ev.Data["severity"]) + "|" + util.StringValue(ev.Data["title"])
			if _, ok := l.seenFindings[key]; ok {
				continue
			}
			l.seenFindings[key] = struct{}{}
			l.findings++
			fmt.Printf("%s FINDING [%s] %s\n", yellow(glyph("finding")), strings.ToUpper(util.StringValue(ev.Data["severity"])), ev.Data["title"])
		case event.CerebrumComplete:
			fmt.Println(green(glyph("complete") + " Analysis complete"))
		case event.CerebrumThought:
			if text := util.StringValue(ev.Data["text"]); strings.HasPrefix(text, "[parse]") || strings.HasPrefix(text, "[domain]") || strings.HasPrefix(text, "[phase]") {
				fmt.Printf("%s %s\n", dim(glyph("brain")), dim(text))
			}
		case event.CerebrumError:
			fmt.Printf("%s Cerebrum error: %v\n", red("ERROR:"), ev.Data["message"])
		case event.Error:
			fmt.Printf("%s %v\n", red("ERROR:"), ev.Data["message"])
		}
	}
}

func (l *cliLogger) printProgressLine() {
	elapsed := time.Since(l.start)
	bar := progressBar(l.round, l.cfg.Analysis.MaxRounds, 24)
	fmt.Printf("%s Round %d/%d %s | drones:%d | H:%d F:%d T:%d | %s\n",
		color.CyanString(glyph("brain")),
		l.round, l.cfg.Analysis.MaxRounds,
		bar,
		l.activeDrones,
		l.hypotheses, l.findings, l.tasks,
		color.New(color.FgWhite, color.Faint).Sprintf("%02d:%02d", int(elapsed.Minutes()), int(elapsed.Seconds())%60),
	)
}

func (l *cliLogger) printFinal(cfg *config.Config) {
	fmt.Println()
	elapsed := time.Since(l.start)
	green := color.New(color.FgGreen, color.Bold).SprintFunc()
	fmt.Printf("%s Done in %02d:%02d\n", green(glyph("check")), int(elapsed.Minutes()), int(elapsed.Seconds())%60)
	fmt.Printf("   Hypotheses: %d  Findings: %d  Tasks: %d\n", l.hypotheses, l.findings, l.tasks)
	if !flags.noReport {
		store := core.NewJSONStore(cfg.Paths.Workspace)
		fmt.Printf("   Reports: %s\n", filepath.Join(store.WorkspacePath(l.target), "reports"))
		fmt.Println(reportFilesSummary(cfg, l.target))
	}
}

func progressBar(current, total, width int) string {
	if total <= 0 {
		total = 1
	}
	if current > total {
		current = total
	}
	filled := current * width / total
	var b strings.Builder
	b.WriteString("[")
	for i := 0; i < width; i++ {
		if i < filled {
			b.WriteString("=")
		} else if i == filled {
			b.WriteString(">")
		} else {
			b.WriteString(" ")
		}
	}
	b.WriteString("]")
	return b.String()
}
