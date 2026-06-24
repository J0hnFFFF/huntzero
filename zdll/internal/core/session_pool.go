package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"zdll/internal/llm"
)

// ErrNoSlot is returned by Acquire when no idle session slot is available.
var ErrNoSlot = errors.New("no idle session slot available")

// defaultRoles is the default set of drone roles when none are provided.
var defaultRoles = []string{
	"topology-mapper",
	"data-flow-tracer",
	"state-validator",
	"evidence-collector",
	"verifier",
	"scope-definer",
	"semantic-analyzer",
	"exploit-crafter",
	"investigator",
	"poc-generator",
	"harness-generator",
	"crash-analyzer",
	"doc-analyst",
	"general",
}

// AgentSession is a reusable LLM session that can accept prompts.
type AgentSession interface {
	Prompt(ctx context.Context, prompt string) (string, error)
	Close() error
}

type poolSlot struct {
	role       string
	idx        int
	session    AgentSession
	inUse      bool
	useCount   int
	sandboxDir string
	agentFile  string
}

// DroneSessionPool maintains pre-warmed reusable sessions grouped by drone role.
type DroneSessionPool struct {
	RootDir         string
	Config          any
	PoolSizePerRole int
	Roles           []string
	Timeout         time.Duration

	pools       map[string][]*poolSlot
	semaphores  map[string]chan struct{}
	initialized bool
	mu          sync.Mutex
}

// NewDroneSessionPool creates a new session pool for the given roles.
// If roles is empty, defaultRoles is used.
func NewDroneSessionPool(rootDir string, poolSize int, roles []string, baseConfig *llm.AgentConfig) *DroneSessionPool {
	if poolSize <= 0 {
		poolSize = 2
	}
	if len(roles) == 0 {
		roles = defaultRoles
	}
	return &DroneSessionPool{
		RootDir:         rootDir,
		Config:          baseConfig,
		PoolSizePerRole: poolSize,
		Roles:           roles,
		Timeout:         60 * time.Second,
		pools:           make(map[string][]*poolSlot),
		semaphores:      make(map[string]chan struct{}),
	}
}

// Initialize pre-warms poolSize sessions for each role.
// The runner argument is accepted for API consistency but the pool creates
// reusable sessions directly via the llm package.
func (p *DroneSessionPool) Initialize(ctx context.Context, runner llm.AgentRunner) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.initialized {
		return nil
	}
	if runner == nil {
		return errors.New("runner is nil")
	}

	for _, role := range p.Roles {
		p.semaphores[role] = make(chan struct{}, p.PoolSizePerRole)
		for i := 0; i < p.PoolSizePerRole; i++ {
			slot, _ := p.createSlot(ctx, role, i)
			p.pools[role] = append(p.pools[role], slot)
		}
	}

	p.initialized = true
	return nil
}

func (p *DroneSessionPool) createSlot(ctx context.Context, role string, idx int) (*poolSlot, error) {
	sandboxDir := filepath.Join(p.RootDir, "tmp", ".session_pool", role, fmt.Sprintf("slot-%d", idx))
	if err := os.MkdirAll(sandboxDir, 0o755); err != nil {
		return &poolSlot{role: role, idx: idx, sandboxDir: sandboxDir}, err
	}

	agentFile, err := buildUniqueAgentFile(p.RootDir, role, idx)
	if err != nil {
		return &poolSlot{role: role, idx: idx, sandboxDir: sandboxDir}, err
	}

	cfg := llm.AgentConfig{
		WorkDir:     sandboxDir,
		AgentFile:   agentFile,
		AutoApprove: true,
	}
	if c, ok := p.Config.(llm.AgentConfig); ok {
		cfg.Model = c.Model
		cfg.APIKey = c.APIKey
		cfg.BaseURL = c.BaseURL
		cfg.Thinking = c.Thinking
		cfg.ExtraArgs = c.ExtraArgs
	}

	session, err := createSessionWithTimeout(ctx, cfg, p.Timeout)
	if err != nil {
		_ = os.Remove(agentFile)
		return &poolSlot{
			role:       role,
			idx:        idx,
			sandboxDir: sandboxDir,
			agentFile:  agentFile,
		}, err
	}

	return &poolSlot{
		role:       role,
		idx:        idx,
		session:    session,
		sandboxDir: sandboxDir,
		agentFile:  agentFile,
	}, nil
}

func createSessionWithTimeout(ctx context.Context, cfg llm.AgentConfig, timeout time.Duration) (AgentSession, error) {
	if timeout <= 0 {
		s, err := llm.NewKimiSession(cfg)
		if err != nil {
			return nil, err
		}
		return s, nil
	}

	type result struct {
		session AgentSession
		err     error
	}
	ch := make(chan result, 1)

	go func() {
		s, err := llm.NewKimiSession(cfg)
		ch <- result{session: s, err: err}
	}()

	select {
	case res := <-ch:
		return res.session, res.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(timeout):
		return nil, context.DeadlineExceeded
	}
}

// Acquire borrows an idle session for the given role.
// It returns the session, the slot's sandbox directory, a release function,
// and an error. If no slot is available, ErrNoSlot is returned.
func (p *DroneSessionPool) Acquire(ctx context.Context, role string) (AgentSession, string, func(), error) {
	p.mu.Lock()
	if !p.initialized {
		p.mu.Unlock()
		return nil, "", nil, errors.New("pool not initialized")
	}
	p.mu.Unlock()

	sem, ok := p.semaphores[role]
	if !ok {
		return nil, "", nil, fmt.Errorf("unknown role: %s", role)
	}

	select {
	case sem <- struct{}{}:
	case <-ctx.Done():
		return nil, "", nil, ctx.Err()
	}

	p.mu.Lock()
	slots := p.pools[role]
	for _, slot := range slots {
		if !slot.inUse && slot.session != nil {
			slot.inUse = true
			slot.useCount++
			sandboxDir := slot.sandboxDir
			release := func() {
				p.mu.Lock()
				slot.inUse = false
				p.mu.Unlock()
				<-sem
			}
			p.mu.Unlock()
			return slot.session, sandboxDir, release, nil
		}
	}
	p.mu.Unlock()

	// No idle slot; release the semaphore and return ErrNoSlot.
	<-sem
	return nil, "", nil, ErrNoSlot
}

// HealthCheck pings each idle session with a 2s timeout and replaces dead ones.
func (p *DroneSessionPool) HealthCheck(ctx context.Context, runner llm.AgentRunner) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.initialized {
		return errors.New("pool not initialized")
	}

	for _, role := range p.Roles {
		for i, slot := range p.pools[role] {
			if slot.inUse || slot.session == nil {
				continue
			}
			pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			_, err := slot.session.Prompt(pingCtx, "ping")
			cancel()
			if err == nil {
				continue
			}

			_ = slot.session.Close()
			newSlot, createErr := p.createSlot(ctx, role, slot.idx)
			if createErr == nil && newSlot.session != nil {
				p.pools[role][i] = newSlot
			}
		}
	}
	return nil
}

// Shutdown closes all sessions and removes their agent YAML files.
func (p *DroneSessionPool) Shutdown() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, slots := range p.pools {
		for _, slot := range slots {
			if slot.session != nil {
				_ = slot.session.Close()
			}
			if slot.agentFile != "" {
				_ = os.Remove(slot.agentFile)
			}
		}
	}
	p.pools = make(map[string][]*poolSlot)
	p.semaphores = make(map[string]chan struct{})
	p.initialized = false
	return nil
}

// EnsureAcquireTargetLink creates or updates sandboxDir/_target as a symlink to targetDir.
func EnsureAcquireTargetLink(sandboxDir, targetDir string) error {
	return ensureSymlink(filepath.Join(sandboxDir, "_target"), targetDir)
}

// buildUniqueAgentFile creates a distinct agent YAML file per pool slot by
// reusing llm.BuildAgentYAML and copying its output to a unique path.
func buildUniqueAgentFile(rootDir, role string, idx int) (string, error) {
	baseAgentFile, err := llm.BuildAgentYAML(rootDir, role, "")
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(baseAgentFile)
	if err != nil {
		return "", err
	}
	uniquePath := filepath.Join(
		rootDir, "tmp",
		fmt.Sprintf("pool_%s_slot%d_%d.yaml", sanitizeTaskID(role), idx, time.Now().UnixNano()),
	)
	if err := os.WriteFile(uniquePath, data, 0o644); err != nil {
		return "", err
	}
	return uniquePath, nil
}
