package core

import "testing"

func TestDomainContext_AnalogyPrompts(t *testing.T) {
	dc := NewDomainContext("")
	dc.Domains = []string{"ai-agent", "web"}

	prompt := dc.AnalogyPrompts()
	if prompt == "" {
		t.Fatal("expected analogy prompt for ai-agent+web")
	}
	if !containsString(prompt, "request-as-proxy") {
		t.Errorf("expected web first-principle analogy, got:\n%s", prompt)
	}
}

func TestDomainContext_AnalogyPrompts_SingleDomain(t *testing.T) {
	dc := NewDomainContext("")
	dc.Domains = []string{"web"}
	if dc.AnalogyPrompts() != "" {
		t.Error("expected no analogy prompt with a single domain")
	}
}
