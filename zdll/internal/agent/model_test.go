package agent

import "testing"

func TestDetectProvider(t *testing.T) {
	tests := []struct {
		baseURL string
		want    string
	}{
		{"", "anthropic"},
		{"https://api.kimi.com/coding", "anthropic"},
		{"https://api.kimi.com/coding/", "anthropic"},
		{"https://api.kimi.com/coding/v1", "openai"},
		{"https://api.kimi.com/coding/v1/", "openai"},
		{"https://api.anthropic.com/v1/messages", "anthropic"},
		{"https://api.openai.com/v1", "openai"},
		{"https://api.deepseek.com/v1", "openai"},
	}

	for _, tt := range tests {
		t.Run(tt.baseURL, func(t *testing.T) {
			got := DetectProvider(tt.baseURL)
			if got != tt.want {
				t.Errorf("DetectProvider(%q) = %q, want %q", tt.baseURL, got, tt.want)
			}
		})
	}
}
