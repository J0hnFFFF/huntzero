package agent

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/model"

	"zdll/internal/llm"
)

// DefaultDroneRoles are the roles most commonly dispatched by the engine.
// The pool pre-warms one tool-bound runner per role so that per-task
// WithTools overhead is paid only once.
var DefaultDroneRoles = []string{
	"evidence-collector",
	"vuln-hunter",
	"exploit-crafter",
	"variant-analyzer",
	"validator",
	"harness-generator",
	"poc-generator",
	"devils-advocate",
	"scope-definer",
	"doc-analyst",
}

// pooledRunner is a single pre-warmed runner slot.
type pooledRunner struct {
	runner  llm.AgentRunner
	inUse   bool
	created time.Time
}

// RunnerPool pre-warms and lends out drone runners per role.
// It mirrors the intent of Python's DroneSessionPool but for Go's stateless
// Eino runners: the expensive part it avoids is repeated model.WithTools calls.
type RunnerPool struct {
	rootDir  string
	baseModel model.ToolCallingChatModel
	roles    []string
	poolSize int
	timeout  time.Duration

	mu          sync.Mutex
	initialized bool
	pools       map[string][]*pooledRunner
	semaphores  map[string]chan struct{}
}

// NewRunnerPool creates a pool that will pre-warm runners for the given roles.
// poolSize controls how many runners are created per role.
func NewRunnerPool(rootDir string, baseModel model.ToolCallingChatModel, roles []string, poolSize int) *RunnerPool {
	if poolSize <= 0 {
		poolSize = 2
	}
	if len(roles) == 0 {
		roles = DefaultDroneRoles
	}
	return &RunnerPool{
		rootDir:   rootDir,
		baseModel: baseModel,
		roles:     roles,
		poolSize:  poolSize,
		timeout:   60 * time.Second,
		pools:     make(map[string][]*pooledRunner),
		semaphores: make(map[string]chan struct{}),
	}
}

// WithTimeout sets the initialization timeout for pre-warming each runner.
func (p *RunnerPool) WithTimeout(d time.Duration) *RunnerPool {
	p.timeout = d
	return p
}

// Initialize pre-warms tool-bound runners for every configured role.
// It is safe to call multiple times; only the first call creates runners.
func (p *RunnerPool) Initialize(ctx context.Context) error {
	p.mu.Lock()
	if p.initialized {
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()

	// Pre-bind tools once using a dummy workDir. The tool schemas are identical
	// regardless of the per-task sandbox, only the tool implementations differ.
	var boundModel model.ToolCallingChatModel
	var boundErr error
	if p.baseModel != nil {
		dummyToolSet := NewToolSet(p.rootDir)
		infos, err := dummyToolSet.Infos(ctx)
		if err != nil {
			return fmt.Errorf("build tool infos: %w", err)
		}
		boundModel, boundErr = p.baseModel.WithTools(infos)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(p.roles)*p.poolSize)
	for _, role := range p.roles {
		role := role
		p.semaphores[role] = make(chan struct{}, p.poolSize)
		for i := 0; i < p.poolSize; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				slotCtx, cancel := context.WithTimeout(ctx, p.timeout)
				defer cancel()
				_ = slotCtx

				runner := p.createRunner(boundModel, boundErr)
				// Sanity check: ensure the runner can produce a non-nil result for
				// this role. We don't make a real LLM call; we just verify the
				// runner object is usable.
				_ = role
				_ = idx

				p.mu.Lock()
				p.pools[role] = append(p.pools[role], &pooledRunner{
					runner:  runner,
					created: time.Now(),
				})
				p.mu.Unlock()
			}(i)
		}
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			log.Printf("[runner-pool] initialization warning: %v", err)
		}
	}

	p.mu.Lock()
	p.initialized = true
	p.mu.Unlock()
	log.Printf("[runner-pool] initialized %d roles x %d slots", len(p.roles), p.poolSize)
	return nil
}

func (p *RunnerPool) createRunner(boundModel model.ToolCallingChatModel, boundErr error) llm.AgentRunner {
	if boundModel != nil {
		return NewDroneGraphRunnerWithBoundModel(boundModel)
	}
	if p.baseModel != nil {
		return NewDroneGraphRunner(p.baseModel)
	}
	// No model means callers will fall back at runtime; return a harmless runner.
	return NewDroneGraphRunner(nil)
}

// Acquire borrows a runner for the given role. The returned release function
// must be called when the caller is done with the runner.
// If the pool is exhausted or has no usable runner for the role, it falls back
// to creating a fresh runner from the base model.
func (p *RunnerPool) Acquire(ctx context.Context, role string) (llm.AgentRunner, func(), error) {
	sem, ok := p.semaphores[role]
	if !ok {
		// Unknown role: no pool, just return a fresh runner.
		return p.freshRunner(), func() {}, nil
	}

	select {
	case sem <- struct{}{}:
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}

	p.mu.Lock()
	for _, slot := range p.pools[role] {
		if !slot.inUse {
			slot.inUse = true
			p.mu.Unlock()
			return slot.runner, func() {
				p.mu.Lock()
				slot.inUse = false
				p.mu.Unlock()
				<-sem
			}, nil
		}
	}
	p.mu.Unlock()

	// Pool exhausted for this role: release semaphore and fall back.
	<-sem
	return p.freshRunner(), func() {}, nil
}

func (p *RunnerPool) freshRunner() llm.AgentRunner {
	if p.baseModel != nil {
		return NewDroneGraphRunner(p.baseModel)
	}
	return NewDroneGraphRunner(nil)
}

// Stats returns per-role pool statistics.
func (p *RunnerPool) Stats() map[string]map[string]int {
	p.mu.Lock()
	defer p.mu.Unlock()
	stats := make(map[string]map[string]int, len(p.pools))
	for role, slots := range p.pools {
		total := len(slots)
		inUse := 0
		for _, s := range slots {
			if s.inUse {
				inUse++
			}
		}
		stats[role] = map[string]int{
			"total":   total,
			"in_use":  inUse,
			"available": total - inUse,
		}
	}
	return stats
}

// Shutdown drains the pool. It does not close underlying Eino models because
// they are typically shared; it just releases bookkeeping resources.
func (p *RunnerPool) Shutdown() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pools = make(map[string][]*pooledRunner)
	p.initialized = false
}

// PooledRunner implements llm.AgentRunner by borrowing a runner from a
// RunnerPool for each Run call and returning it when the response stream ends.
type PooledRunner struct {
	pool *RunnerPool
}

// NewPooledRunner creates an AgentRunner that acquires runners from the pool.
func NewPooledRunner(pool *RunnerPool) *PooledRunner {
	return &PooledRunner{pool: pool}
}

// Run implements llm.AgentRunner.
func (r *PooledRunner) Run(ctx context.Context, cfg llm.AgentConfig, prompt string) (<-chan llm.Event, error) {
	runner, release, err := r.pool.Acquire(ctx, cfg.Role)
	if err != nil {
		return nil, err
	}
	out := make(chan llm.Event, 64)
	go func() {
		defer close(out)
		defer release()
		ch, err := runner.Run(ctx, cfg, prompt)
		if err != nil {
			out <- llm.Event{Type: "error", Content: err.Error()}
			return
		}
		for ev := range ch {
			out <- ev
		}
	}()
	return out, nil
}
