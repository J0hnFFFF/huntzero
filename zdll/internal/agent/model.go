package agent

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/components/model/claude"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
)

// ModelConfig configures an Eino chat model. It supports both OpenAI-compatible
// and Anthropic Messages API providers, which lets zdll talk to Kimi Code via
// either endpoint.
type ModelConfig struct {
	Provider string // "openai" or "anthropic"; empty defaults to "openai"
	APIKey   string
	BaseURL  string
	Model    string
	Timeout  time.Duration
	// Headers are attached to every HTTP request sent to the model endpoint.
	// They can be used to override User-Agent or add provider-specific headers.
	Headers map[string]string
}

// headerTransport wraps an http.RoundTripper and injects custom headers.
type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	return t.base.RoundTrip(req)
}

// NewChatModel creates an Eino chat model backed by the configured provider.
// It wires for Kimi by default but works with any compatible endpoint.
func NewChatModel(ctx context.Context, cfg ModelConfig) (model.ToolCallingChatModel, error) {
	cfg.Provider = strings.ToLower(cfg.Provider)
	if cfg.Provider == "" || cfg.Provider == "kimi" {
		cfg.Provider = DetectProvider(cfg.BaseURL)
	}

	switch cfg.Provider {
	case "anthropic":
		return newAnthropicChatModel(ctx, cfg)
	case "openai":
		return newOpenAIChatModel(ctx, cfg)
	default:
		return nil, fmt.Errorf("unsupported llm provider %q (use openai or anthropic)", cfg.Provider)
	}
}

func newOpenAIChatModel(ctx context.Context, cfg ModelConfig) (model.ToolCallingChatModel, error) {
	if cfg.Model == "" {
		cfg.Model = "kimi-for-coding"
	}
	// Normalize fully-qualified model names like "kimi-code/kimi-for-coding"
	// to the short name expected by the OpenAI-compatible endpoint.
	if idx := strings.LastIndex(cfg.Model, "/"); idx != -1 {
		cfg.Model = cfg.Model[idx+1:]
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.kimi.com/coding/v1"
	}

	chatModelCfg := &openai.ChatModelConfig{
		APIKey:  cfg.APIKey,
		BaseURL: cfg.BaseURL,
		Model:   cfg.Model,
		Timeout: cfg.Timeout,
	}

	if len(cfg.Headers) > 0 {
		chatModelCfg.HTTPClient = newHeaderHTTPClient(cfg.Headers, cfg.Timeout)
	}

	chatModel, err := openai.NewChatModel(ctx, chatModelCfg)
	if err != nil {
		return nil, fmt.Errorf("create eino openai chat model: %w", err)
	}
	return chatModel, nil
}

func newAnthropicChatModel(ctx context.Context, cfg ModelConfig) (model.ToolCallingChatModel, error) {
	if cfg.Model == "" {
		cfg.Model = "kimi-for-coding"
	}
	if idx := strings.LastIndex(cfg.Model, "/"); idx != -1 {
		cfg.Model = cfg.Model[idx+1:]
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.kimi.com/coding"
	}

	maxTokens := 16384
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}

	baseURL := cfg.BaseURL
	disableParallel := true
	chatModelCfg := &claude.Config{
		APIKey:                 cfg.APIKey,
		BaseURL:                &baseURL,
		Model:                  cfg.Model,
		MaxTokens:              maxTokens,
		HTTPClient:             newHeaderHTTPClient(cfg.Headers, timeout),
		AdditionalHeaderFields: cfg.Headers,
		DisableParallelToolUse: &disableParallel,
	}

	chatModel, err := claude.NewChatModel(ctx, chatModelCfg)
	if err != nil {
		return nil, fmt.Errorf("create eino anthropic chat model: %w", err)
	}
	return chatModel, nil
}

func newHeaderHTTPClient(headers map[string]string, timeout time.Duration) *http.Client {
	if len(headers) == 0 {
		return &http.Client{Timeout: timeout}
	}
	return &http.Client{
		Transport: &headerTransport{
			base:    http.DefaultTransport,
			headers: headers,
		},
		Timeout: timeout,
	}
}

// DetectProvider chooses a sensible provider when none is explicitly set.
// It preserves backward compatibility for configs that only specify a Kimi
// Code base URL.
func DetectProvider(baseURL string) string {
	if baseURL == "" {
		return "anthropic"
	}
	u := strings.ToLower(baseURL)
	// Kimi Code OpenAI-compatible endpoint ends in /coding/v1.
	if strings.HasSuffix(u, "/coding/v1") || strings.Contains(u, "/coding/v1/") {
		return "openai"
	}
	// Kimi Code Anthropic-compatible endpoint is /coding (no /v1).
	if strings.Contains(u, "api.kimi.com/coding") {
		return "anthropic"
	}
	// Generic Anthropic base URL.
	if strings.Contains(u, "anthropic") {
		return "anthropic"
	}
	return "openai"
}
