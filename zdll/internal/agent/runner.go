package agent

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"zdll/internal/llm"
)

// CerebrumRunner implements llm.AgentRunner using a plain Eino ChatModel.
// It is used for the strategic synthesis loop where the model emits JSON.
type CerebrumRunner struct {
	model       model.ToolCallingChatModel
	extraFields map[string]any
	memory      bool
	history     []*schema.Message
}

// NewCerebrumRunner creates a runner for single-shot LLM calls.
func NewCerebrumRunner(m model.ToolCallingChatModel) *CerebrumRunner {
	return &CerebrumRunner{model: m}
}

// WithExtraFields attaches provider-specific extra body fields to every Cerebrum
// request. This is used for OpenAI-compatible providers to force JSON output.
func (r *CerebrumRunner) WithExtraFields(extra map[string]any) *CerebrumRunner {
	r.extraFields = extra
	return r
}

// WithMemory enables conversational memory across rounds. When enabled, prior
// assistant responses are kept in the message history so the model sees the
// full conversation context, mirroring a persistent SDK session.
func (r *CerebrumRunner) WithMemory(enabled bool) *CerebrumRunner {
	r.memory = enabled
	return r
}

// Run sends the prompt to the model and streams the text response.
func (r *CerebrumRunner) Run(ctx context.Context, cfg llm.AgentConfig, prompt string) (<-chan llm.Event, error) {
	out := make(chan llm.Event, 64)
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				out <- llm.Event{Type: "error", Content: fmt.Sprintf("cerebrum runner panic: %v\n%s", rec, string(debug.Stack()))}
			}
			close(out)
		}()

		messages := []*schema.Message{
			schema.SystemMessage("You are an expert security auditor. Follow the user's instructions exactly and produce the requested output format."),
		}
		if r.memory {
			messages = append(messages, r.history...)
		}
		messages = append(messages, schema.UserMessage(prompt))

		var opts []model.Option
		if len(r.extraFields) > 0 {
			opts = append(opts, openai.WithExtraFields(r.extraFields))
		}

		stream, err := r.model.Stream(ctx, messages, opts...)
		if err != nil {
			out <- llm.Event{Type: "error", Content: err.Error()}
			return
		}
		defer stream.Close()

		var fullResponse strings.Builder
		for {
			msg, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				out <- llm.Event{Type: "error", Content: err.Error()}
				return
			}
			if msg != nil && msg.Content != "" {
				fullResponse.WriteString(msg.Content)
				out <- llm.Event{Type: "text", Content: msg.Content}
			}
		}
		if r.memory {
			// Append the user prompt and the assistant response to history so
			// subsequent rounds retain conversational context. Cap history to
			// the most recent 5 rounds to avoid unbounded context growth.
			r.history = append(r.history, schema.UserMessage(prompt))
			r.history = append(r.history, schema.AssistantMessage(fullResponse.String(), nil))
			if len(r.history) > 10 {
				r.history = r.history[len(r.history)-10:]
			}
		}
		out <- llm.Event{Type: "done", Content: ""}
	}()
	return out, nil
}

// DroneRunner implements llm.AgentRunner as a tool-calling agent.
// It runs a custom ReAct loop instead of Eino's react.Agent to work around
// compatibility issues between the Kimi Code Anthropic endpoint and the
// Eino react graph (tool_call_id mismatches / max-step errors).
type DroneRunner struct {
	model model.ToolCallingChatModel
}

// NewDroneRunner creates a runner backed by a custom tool-calling loop.
func NewDroneRunner(m model.ToolCallingChatModel) *DroneRunner {
	return &DroneRunner{model: m}
}

const droneMaxSteps = 15

// Run executes the prompt with tool access and returns the final text.
func (r *DroneRunner) Run(ctx context.Context, cfg llm.AgentConfig, prompt string) (<-chan llm.Event, error) {
	out := make(chan llm.Event, 64)
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				out <- llm.Event{Type: "error", Content: fmt.Sprintf("drone runner panic: %v\n%s", rec, string(debug.Stack()))}
			}
			close(out)
		}()

		workDir := cfg.WorkDir
		if workDir == "" {
			workDir = "."
		}

		log.Printf("[drone-runner] building tool set for workDir=%s", workDir)
		toolSet := NewToolSet(workDir)

		toolInfos, err := toolSet.Infos(ctx)
		if err != nil {
			log.Printf("[drone-runner] tool infos failed: %v", err)
			out <- llm.Event{Type: "error", Content: fmt.Sprintf("tool infos: %v", err)}
			return
		}
		log.Printf("[drone-runner] binding %d tools", len(toolInfos))

		modelWithTools, err := r.model.WithTools(toolInfos)
		if err != nil {
			log.Printf("[drone-runner] WithTools failed: %v", err)
			out <- llm.Event{Type: "error", Content: fmt.Sprintf("bind tools: %v", err)}
			return
		}

		systemPrompt := buildDroneSystemPrompt(cfg.Role, workDir)
		messages := []*schema.Message{
			schema.SystemMessage(systemPrompt),
			schema.UserMessage(prompt),
		}

		var finalText string
		var pendingToolCalls bool
		for step := 0; step < droneMaxSteps; step++ {
			log.Printf("[drone-runner] step %d calling model.Generate (messages=%d)", step+1, len(messages))
			resp, err := modelWithTools.Generate(ctx, messages)
			if err != nil {
				log.Printf("[drone-runner] model.Generate failed: %v", err)
				out <- llm.Event{Type: "error", Content: err.Error()}
				return
			}
			if resp == nil {
				log.Printf("[drone-runner] model.Generate returned nil response")
				out <- llm.Event{Type: "error", Content: "model returned nil response"}
				return
			}

			messages = append(messages, resp)
			finalText = resp.Content
			pendingToolCalls = len(resp.ToolCalls) > 0

			if len(resp.ToolCalls) == 0 {
				log.Printf("[drone-runner] step %d produced final answer (%d chars)", step+1, len(resp.Content))
				break
			}

			log.Printf("[drone-runner] step %d model requested %d tool call(s)", step+1, len(resp.ToolCalls))
			for _, tc := range resp.ToolCalls {
				log.Printf("[drone-runner] executing tool %s (id=%s)", tc.Function.Name, tc.ID)
				result, toolErr := toolSet.Execute(ctx, tc)
				if toolErr != nil {
					result = fmt.Sprintf("[error] %v", toolErr)
				}
				if strings.TrimSpace(result) == "" {
					result = "[no output]"
				}
				// NOTE: Individual tools (read_file, bash, grep_search) already enforce
				// their own size limits and append truncation notices. Do not truncate
				// again here, otherwise drones lose access to code that appears later
				// in large files (e.g. authentication checks at line 500+).
				messages = append(messages, schema.ToolMessage(result, tc.ID))
				log.Printf("[drone-runner] tool %s result=%d chars", tc.Function.Name, len(result))
			}
		}

		if pendingToolCalls {
			errStr := fmt.Sprintf("drone did not produce a final answer within %d steps", droneMaxSteps)
			log.Printf("[drone-runner] %s", errStr)
			out <- llm.Event{Type: "error", Content: errStr}
			return
		}

		log.Printf("[drone-runner] final response=%d chars", len(finalText))
		out <- llm.Event{Type: "text", Content: finalText}
		out <- llm.Event{Type: "done", Content: ""}
	}()
	return out, nil
}

// buildDroneSystemPrompt creates the system prompt for a drone task.
// It preserves the safety constraints from the original Kimi CLI agent spec.
func buildDroneSystemPrompt(role, workDir string) string {
	var b strings.Builder

	botsPath := filepath.Join(workDir, "..", "..", ".bots.md")
	if data, err := os.ReadFile(botsPath); err == nil && len(data) > 0 {
		b.WriteString(string(data))
		b.WriteString("\n\n")
	}

	fmt.Fprintf(&b, "[DRONE ROLE: %s]\n", role)
	b.WriteString("You are a security analysis drone operating inside a sandbox. " +
		"You have access to bash, read_file, write_file, grep_search, glob, and fetch_url tools. " +
		"Use these tools to investigate the task and return findings in the requested format.\n\n")

	// The drone may run either in a sandbox (with a _target symlink/junction) or
	// directly in the project root when sandbox isolation is unavailable. Tailor
	// the path guidance to whichever layout is actually present.
	targetPrefix := ""
	if info, err := os.Stat(filepath.Join(workDir, "_target")); err == nil && info.IsDir() {
		targetPrefix = "_target/"
	}
	if targetPrefix != "" {
		b.WriteString("The project source is located under the `_target/` subdirectory of your current working directory. " +
			"Always reference source files with the `_target/` prefix, e.g. `_target/src/...` or `_target/*.ts`. " +
			"Do not use bare top-level relative paths such as `src/...` or `*.ts`. " +
			"If a file is reported missing, list the directory with `glob` or `bash` to verify the exact name and casing before retrying.\n\n")
	} else {
		b.WriteString("The project source is in your current working directory. " +
			"Use relative paths such as `src/...` or `*.ts` directly. " +
			"Do not assume a `_target/` subdirectory exists.\n\n")
	}

	b.WriteString(`[HARD CONSTRAINTS]
`)
	b.WriteString("1. Use grep_search / bash to prove findings with concrete code evidence.\n")
	b.WriteString("2. NEVER run dangerous or destructive exploits (e.g. rm -rf, format drives).\n")
	b.WriteString("3. Write PoC/exploit/report files ONLY to ./pocs/ under the working directory.\n")
	b.WriteString("4. DO NOT start long-running servers or heavy frameworks unless instructed.\n")
	b.WriteString("5. Base conclusions on verifiable code structure, not speculation.\n")
	b.WriteString("6. If a tool fails twice, change approach or stop.\n")
	b.WriteString("7. You are a concise investigator: gather evidence in at most 3-5 tool calls,\n")
	b.WriteString("   then immediately produce the final answer in the user's requested format.\n")
	return b.String()
}
