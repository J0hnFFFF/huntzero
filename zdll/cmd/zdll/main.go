package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

	"zdll/internal/app"
	"zdll/internal/config"

	"zdll/internal/core"
	"zdll/internal/event"
	"zdll/internal/eventbus"
	"zdll/internal/llm"
	"zdll/internal/report"
	"zdll/internal/scanner"
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
	diffBase         string
	generateBaseline string
	summaryFormat    string
}

var flags cliFlags

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
		Version: "0.1.0",
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
	cmd.Flags().StringVar(&flags.failOnSeverity, "fail-on", "", "exit non-zero if findings at or above severity")
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
	cmd.Flags().StringVar(&flags.failOnSeverity, "fail-on", "high", "exit non-zero if findings at or above severity")
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
		return fmt.Errorf("KIMI_API_KEY is required (env var or config file)")
	}

	bus := eventbus.NewLocal()
	application := app.New(cfg, bus, core.NewJSONStore(cfg.Paths.Workspace), llm.NewKimiRunner())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	setupSignalHandler(cancel)

	opts, changedPaths, err := buildScanOptions(cfg, target)
	if err != nil {
		return err
	}

	start := time.Now()
	logger := startEventLogger(bus, cfg, target)

	bm, err := application.Scan(ctx, target, flags.resume, opts...)
	if err != nil {
		return err
	}

	if !waitForScanComplete(ctx, bus) {
		return nil
	}

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
			printFinalStats(bm.Stats(), cfg, target, elapsed)
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
		return fmt.Errorf("KIMI_API_KEY is not set")
	}
	masked := cfg.LLM.APIKey
	if len(masked) > 8 {
		masked = masked[:4] + "..." + masked[len(masked)-4:]
	}
	fmt.Fprintf(out, "API Key:  %s (set)\n", masked)
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

func printFinalStats(stats map[string]int, cfg *config.Config, target string, elapsed time.Duration) {
	green := color.New(color.FgGreen, color.Bold).SprintFunc()
	fmt.Println()
	fmt.Printf("%s Done in %s\n", green("✔"), elapsed.Round(time.Second))
	fmt.Printf("   Hypotheses: %d  Findings: %d  Tasks: %d\n", stats["total_hypotheses"], stats["total_findings"], stats["total_tasks"])
	if !flags.noReport {
		store := core.NewJSONStore(cfg.Paths.Workspace)
		fmt.Printf("   Reports: %s\n", filepath.Join(store.WorkspacePath(target), "reports"))
	}
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
}

func newCLILogger(ch <-chan event.Event, cfg *config.Config, target string) *cliLogger {
	return &cliLogger{ch: ch, cfg: cfg, target: target, start: time.Now()}
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
			l.round = intValue(ev.Data["round"])
			l.printProgressLine()
		case event.HypothesisGenerated:
			l.hypotheses++
			fmt.Printf("%s %s %s\n", purple("🧬"), dim(fmt.Sprintf("%s [%.0f%%]", ev.Data["id"], floatValue(ev.Data["confidence"])*100)), ev.Data["description"])
		case event.HypothesisUpdated:
			fmt.Printf("%s %s → %s\n", purple("🧬"), ev.Data["id"], dim(fmt.Sprintf("%v", ev.Data["status"])))
		case event.TaskQueued:
			l.tasks++
			fmt.Printf("%s %s queued (%s)\n", dim("🚁"), ev.Data["id"], ev.Data["drone_role"])
		case event.DroneLaunched:
			l.activeDrones++
		case event.DroneCompleted, event.DroneFailed:
			if l.activeDrones > 0 {
				l.activeDrones--
			}
			if ev.Type == event.DroneFailed {
				fmt.Printf("%s Drone failed: %v\n", red("✖"), ev.Data["error"])
			}
		case event.FindingConfirmed:
			l.findings++
			fmt.Printf("%s FINDING [%s] %s\n", yellow("🟠"), strings.ToUpper(fmt.Sprintf("%v", ev.Data["severity"])), ev.Data["title"])
		case event.CerebrumComplete:
			fmt.Println(green("✅ Analysis complete"))
		case event.Error:
			fmt.Printf("%s %v\n", red("ERROR:"), ev.Data["message"])
		}
	}
}

func (l *cliLogger) printProgressLine() {
	elapsed := time.Since(l.start)
	bar := progressBar(l.round, l.cfg.Analysis.MaxRounds, 24)
	fmt.Printf("%s Round %d/%d %s | drones:%d | H:%d F:%d T:%d | %s\n",
		color.CyanString("🧠"),
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
	fmt.Printf("%s Done in %02d:%02d\n", green("✔"), int(elapsed.Minutes()), int(elapsed.Seconds())%60)
	fmt.Printf("   Hypotheses: %d  Findings: %d  Tasks: %d\n", l.hypotheses, l.findings, l.tasks)
	if !flags.noReport {
		store := core.NewJSONStore(cfg.Paths.Workspace)
		fmt.Printf("   Reports: %s\n", filepath.Join(store.WorkspacePath(l.target), "reports"))
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

func intValue(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

func floatValue(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	}
	return 0
}
