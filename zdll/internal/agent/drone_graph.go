package agent

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"zdll/internal/llm"
)

// DroneGraphRunner implements a tool-calling ReAct loop using Eino's compose
// graph instead of the pre-built react.Agent. This keeps us inside the Eino
// ecosystem (model, schema, tools, state) while giving explicit control over
// message history accumulation and step limits.
type DroneGraphRunner struct {
	model       model.ToolCallingChatModel
	boundModel  model.ToolCallingChatModel // optional pre-bound model (from a pool)
	preboundErr error                      // error from pre-binding tools, if any
}

// NewDroneGraphRunner creates a runner backed by an Eino compose graph.
func NewDroneGraphRunner(m model.ToolCallingChatModel) *DroneGraphRunner {
	return &DroneGraphRunner{model: m}
}

// NewDroneGraphRunnerWithBoundModel creates a runner that reuses an already
// tool-bound model. This avoids the cost of calling WithTools on every task
// when the runner comes from a pre-warmed pool.
func NewDroneGraphRunnerWithBoundModel(boundModel model.ToolCallingChatModel) *DroneGraphRunner {
	return &DroneGraphRunner{boundModel: boundModel}
}

// droneState is the per-run shared state used by the compose graph.
type droneState struct {
	messages   []*schema.Message
	steps      int
	toolRounds int
}

const (
	droneGraphMaxSteps       = 50
	droneGraphSoftToolRounds = 6
)

const softStopMessage = "You have already used enough tool calls. Stop using tools and provide your final answer immediately."

// Run executes the prompt with tool access and returns the final text.
func (r *DroneGraphRunner) Run(ctx context.Context, cfg llm.AgentConfig, prompt string) (<-chan llm.Event, error) {
	out := make(chan llm.Event, 64)
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				out <- llm.Event{Type: "error", Content: fmt.Sprintf("drone graph runner panic: %v\n%s", rec, string(debug.Stack()))}
			}
			close(out)
		}()

		workDir := cfg.WorkDir
		if workDir == "" {
			workDir = "."
		}

		log.Printf("[drone-graph] building tool set for workDir=%s", workDir)
		toolSet := NewToolSet(workDir)

		var modelWithTools model.ToolCallingChatModel
		var bindErr error
		if r.boundModel != nil {
			modelWithTools = r.boundModel
			bindErr = r.preboundErr
			log.Printf("[drone-graph] using pre-bound model")
		} else {
			toolInfos, err := toolSet.Infos(ctx)
			if err != nil {
				log.Printf("[drone-graph] tool infos failed: %v", err)
				out <- llm.Event{Type: "error", Content: fmt.Sprintf("tool infos: %v", err)}
				return
			}
			log.Printf("[drone-graph] binding %d tools", len(toolInfos))
			modelWithTools, bindErr = r.model.WithTools(toolInfos)
		}
		if bindErr != nil {
			log.Printf("[drone-graph] WithTools failed: %v", bindErr)
			out <- llm.Event{Type: "error", Content: fmt.Sprintf("bind tools: %v", bindErr)}
			return
		}

		systemPrompt := buildDroneSystemPrompt(cfg.Role, workDir)
		seedMessages := []*schema.Message{
			schema.SystemMessage(systemPrompt),
			schema.UserMessage(prompt),
		}

		finalMsg, err := r.runGraph(ctx, modelWithTools, toolSet, seedMessages)
		if err != nil {
			log.Printf("[drone-graph] graph run failed: %v", err)
			out <- llm.Event{Type: "error", Content: err.Error()}
			return
		}

		log.Printf("[drone-graph] final response=%d chars", len(finalMsg.Content))
		out <- llm.Event{Type: "text", Content: finalMsg.Content}
		out <- llm.Event{Type: "done", Content: ""}
	}()
	return out, nil
}

func (r *DroneGraphRunner) runGraph(
	ctx context.Context,
	modelWithTools model.ToolCallingChatModel,
	toolSet *ToolSet,
	seedMessages []*schema.Message,
) (*schema.Message, error) {

	genState := func(ctx context.Context) *droneState {
		return &droneState{messages: make([]*schema.Message, 0, 64)}
	}

	g := compose.NewGraph[[]*schema.Message, *schema.Message](compose.WithGenLocalState(genState))

	// Model node: accumulate incoming messages into shared state, then call the
	// bound chat model with the full conversation history.
	err := g.AddChatModelNode("model", modelWithTools,
		compose.WithStatePreHandler(func(ctx context.Context, in []*schema.Message, state *droneState) ([]*schema.Message, error) {
			state.steps++
			if state.steps > droneGraphMaxSteps {
				return nil, fmt.Errorf("drone exceeded maximum %d tool steps", droneGraphMaxSteps)
			}
			state.messages = append(state.messages, in...)

			// Soft limit: after enough tool rounds, firmly remind the model to answer.
			if state.toolRounds >= droneGraphSoftToolRounds {
				if last := state.messages[len(state.messages)-1]; last.Role != schema.User || last.Content != softStopMessage {
					state.messages = append(state.messages, schema.UserMessage(softStopMessage))
				}
			}
			return state.messages, nil
		}),
		compose.WithStatePostHandler(func(ctx context.Context, out *schema.Message, state *droneState) (*schema.Message, error) {
			state.messages = append(state.messages, out)
			if len(out.ToolCalls) > 0 {
				state.toolRounds++
			}
			return out, nil
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("add model node: %w", err)
	}

	// Tools node: execute tool calls produced by the assistant message.
	invokableTools := toolSet.Tools()
	baseTools := make([]tool.BaseTool, len(invokableTools))
	for i, t := range invokableTools {
		baseTools[i] = t
	}

	// Catch tool errors and empty outputs so that every tool_call_id gets a
	// non-empty tool result. The Anthropic/Kimi Code endpoint drops empty tool
	// result blocks and then rejects the next request as missing responses.
	errorToResultMiddleware := func(next compose.InvokableToolEndpoint) compose.InvokableToolEndpoint {
		return func(ctx context.Context, input *compose.ToolInput) (*compose.ToolOutput, error) {
			out, err := next(ctx, input)
			if err != nil {
				log.Printf("[drone-graph] tool %s id=%s error: %v", input.Name, input.CallID, err)
				return &compose.ToolOutput{Result: fmt.Sprintf("[error] %v", err)}, nil
			}
			result := ""
			if out != nil {
				result = out.Result
			}
			if strings.TrimSpace(result) == "" {
				result = "[no output]"
			}
			return &compose.ToolOutput{Result: result}, nil
		}
	}

	toolsNode, err := compose.NewToolNode(ctx, &compose.ToolsNodeConfig{
		Tools: baseTools,
		ToolCallMiddlewares: []compose.ToolMiddleware{
			{Invokable: errorToResultMiddleware},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create tools node: %w", err)
	}

	err = g.AddToolsNode("tools", toolsNode,
		compose.WithStatePostHandler(func(ctx context.Context, out []*schema.Message, state *droneState) ([]*schema.Message, error) {
			for _, m := range out {
				if len(m.Content) > 12000 {
					m.Content = m.Content[:12000] + "\n... [truncated]"
				}
			}
			return out, nil
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("add tools node: %w", err)
	}

	// Layout: START -> model -> (tool calls?) -> tools -> model ... -> END
	err = g.AddEdge(compose.START, "model")
	if err != nil {
		return nil, fmt.Errorf("add start edge: %w", err)
	}

	modelBranch := func(ctx context.Context, msg *schema.Message) (string, error) {
		if len(msg.ToolCalls) > 0 {
			return "tools", nil
		}
		return compose.END, nil
	}
	err = g.AddBranch("model", compose.NewGraphBranch(modelBranch, map[string]bool{
		"tools":     true,
		compose.END: true,
	}))
	if err != nil {
		return nil, fmt.Errorf("add model branch: %w", err)
	}

	err = g.AddEdge("tools", "model")
	if err != nil {
		return nil, fmt.Errorf("add tools edge: %w", err)
	}

	runnable, err := g.Compile(ctx,
		compose.WithGraphName("drone"),
		compose.WithMaxRunSteps(droneGraphMaxSteps),
	)
	if err != nil {
		return nil, fmt.Errorf("compile graph: %w", err)
	}

	return runnable.Invoke(ctx, seedMessages)
}
