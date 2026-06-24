package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// LLMConfig holds provider settings.
type LLMConfig struct {
	Provider string `mapstructure:"provider"`
	APIKey   string `mapstructure:"api_key"`
	BaseURL  string `mapstructure:"base_url"`
	Model    string `mapstructure:"model"`
	Thinking bool   `mapstructure:"thinking"`
}

// AnalysisConfig holds analysis budget defaults.
type AnalysisConfig struct {
	Workers              int  `mapstructure:"workers"`
	MaxRounds            int  `mapstructure:"max_rounds"`
	MaxTasks             int  `mapstructure:"max_tasks"`
	MaxTime              int  `mapstructure:"max_time"`
	Stagnation           int  `mapstructure:"stagnation"`
	AutoApprove          bool `mapstructure:"auto_approve"`
	SectorThresholdFiles int  `mapstructure:"sector_threshold_files"`
}

// PathConfig holds filesystem paths.
type PathConfig struct {
	Workspace  string `mapstructure:"workspace"`
	Skills     string `mapstructure:"skills"`
	OSVScanner string `mapstructure:"osv_scanner"`
}

// Config is the top-level configuration.
type Config struct {
	LLM      LLMConfig      `mapstructure:"llm"`
	Analysis AnalysisConfig `mapstructure:"analysis"`
	Paths    PathConfig     `mapstructure:"paths"`
}

// Default returns a Config with safe defaults.
func Default() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		LLM: LLMConfig{
			Provider: "kimi",
			BaseURL:  "https://api.kimi.com/coding/v1",
			Model:    "kimi-for-coding",
			Thinking: true,
		},
		Analysis: AnalysisConfig{
			Workers:              5,
			MaxRounds:            30,
			MaxTasks:             200,
			MaxTime:              7200,
			Stagnation:           3,
			AutoApprove:          true,
			SectorThresholdFiles: 5000,
		},
		Paths: PathConfig{
			Workspace: filepath.Join(home, "zdll_workspace"),
			Skills:    "./skills",
		},
	}
}

// Load reads configuration from file, environment, and defaults.
func Load(configFile string) (*Config, error) {
	cfg := Default()
	v := viper.New()
	v.SetConfigType("yaml")

	if configFile != "" {
		v.SetConfigFile(configFile)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		v.AddConfigPath(filepath.Join(home, ".config", "zdll"))
		v.SetConfigName("config")
	}

	// Environment variable bindings.
	v.SetEnvPrefix("ZDLL")
	v.AutomaticEnv()

	// Defaults.
	v.SetDefault("llm.provider", cfg.LLM.Provider)
	v.SetDefault("llm.base_url", cfg.LLM.BaseURL)
	v.SetDefault("llm.model", cfg.LLM.Model)
	v.SetDefault("llm.thinking", cfg.LLM.Thinking)
	v.SetDefault("analysis.workers", cfg.Analysis.Workers)
	v.SetDefault("analysis.max_rounds", cfg.Analysis.MaxRounds)
	v.SetDefault("analysis.max_tasks", cfg.Analysis.MaxTasks)
	v.SetDefault("analysis.max_time", cfg.Analysis.MaxTime)
	v.SetDefault("analysis.stagnation", cfg.Analysis.Stagnation)
	v.SetDefault("analysis.auto_approve", cfg.Analysis.AutoApprove)
	v.SetDefault("analysis.sector_threshold_files", cfg.Analysis.SectorThresholdFiles)
	v.SetDefault("paths.workspace", cfg.Paths.Workspace)
	v.SetDefault("paths.skills", cfg.Paths.Skills)

	_ = v.ReadInConfig() // optional
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Override with explicit env vars for secrets.
	if apiKey := os.Getenv("KIMI_API_KEY"); apiKey != "" {
		cfg.LLM.APIKey = apiKey
	}
	if baseURL := os.Getenv("KIMI_BASE_URL"); baseURL != "" {
		cfg.LLM.BaseURL = baseURL
	}
	if model := os.Getenv("KIMI_MODEL_NAME"); model != "" {
		cfg.LLM.Model = model
	}

	// Resolve relative paths.
	if !filepath.IsAbs(cfg.Paths.Workspace) {
		cwd, _ := os.Getwd()
		cfg.Paths.Workspace = filepath.Join(cwd, cfg.Paths.Workspace)
	}
	if !filepath.IsAbs(cfg.Paths.Skills) {
		cwd, _ := os.Getwd()
		cfg.Paths.Skills = filepath.Join(cwd, cfg.Paths.Skills)
	}

	return cfg, nil
}

// DefaultConfigPath returns the default user configuration file path.
func DefaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "zdll", "config.yaml")
}

// Save writes cfg to path as YAML, creating parent directories if needed.
func Save(cfg *Config, path string) error {
	if path == "" {
		path = DefaultConfigPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(path, data, 0o600)
}

// InitConfigDir creates the configuration directory and a sample file.
func InitConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", "zdll")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "config.yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		sample := `# zdll configuration
llm:
  provider: kimi
  # api_key: sk-...                    # or set KIMI_API_KEY env var
  base_url: https://api.kimi.com/coding/v1
  model: kimi-for-coding
  thinking: true

analysis:
  workers: 5
  max_rounds: 30
  max_tasks: 200
  max_time: 7200
  stagnation: 3
  auto_approve: true
  sector_threshold_files: 5000

paths:
  workspace: ~/zdll_workspace
  skills: ./skills
  # osv_scanner: ""           # empty = auto-download
`
		if err := os.WriteFile(path, []byte(sample), 0o600); err != nil {
			return "", err
		}
	}
	return path, nil
}
