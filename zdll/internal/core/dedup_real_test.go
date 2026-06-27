package core

import "testing"

func TestDedupReal_NemoClaw_Dockerfile(t *testing.T) {
	bb := &Blackboard{Hypotheses: map[string]*HypothesisNode{}}
	h1 := NewHypothesisNode("Dockerfile build context injection allows arbitrary Dockerfile instruction injection via controlled build arguments or context files", 0.65, nil)
	bb.Hypotheses[h1.ID] = h1

	dup, score := findSimilarHypothesis(bb, "The patchStagedDockerfile function allows arbitrary Dockerfile instruction injection via user-controlled template parameters that flow unescaped into docker build context", 0.55)
	t.Logf("dup=%s score=%v", dup, score)
	if dup == "" {
		t.Fatal("expected duplicate")
	}
}

func TestDedupReal_NemoClaw_SSRA(t *testing.T) {
	bb := &Blackboard{Hypotheses: map[string]*HypothesisNode{}}
	h1 := NewHypothesisNode("The SSRF protection in nemoclaw/src/blueprint/ssrf.ts can be bypassed via DNS rebinding or URL parsing differential, allowing sandbox-internal network access", 0.65, nil)
	bb.Hypotheses[h1.ID] = h1

	dup, score := findSimilarHypothesis(bb, "The validateEndpointUrl SSRF protection can be bypassed to access sandbox-internal services at 169.254.169.254 or localhost through URL parsing differentials", 0.55)
	t.Logf("dup=%s score=%v", dup, score)
	if dup == "" {
		t.Fatal("expected duplicate")
	}
}
