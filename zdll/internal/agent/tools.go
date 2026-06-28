package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// ToolSet holds a collection of native Eino tools bound to a working directory.
// It is created per Drone task so that tools operate inside the task sandbox.
//
// Note on Eino built-in tools: Eino ADK provides filesystem/shell middleware
// tools (ls, read_file, grep via ripgrep, etc.) in eino/adk/middlewares/filesystem,
// and a local backend in eino-ext/adk/backend/local. We intentionally do NOT use
// them because (1) the local backend is Unix/macOS only and shells out to /bin/sh
// and rg, which breaks Windows; (2) it has no per-task workDir sandbox; and
// (3) its tool schemas differ from the ones tuned for this project. The custom
// implementations below keep the runner cross-platform and sandbox-aware.
type ToolSet struct {
	workDir string
	tools   []tool.InvokableTool
}

// NewToolSet creates a tool set that operates within workDir.
func NewToolSet(workDir string) *ToolSet {
	t := &ToolSet{workDir: workDir}
	t.tools = []tool.InvokableTool{
		&bashTool{workDir: workDir},
		&readFileTool{workDir: workDir},
		&writeFileTool{workDir: workDir},
		&grepTool{workDir: workDir},
		&globTool{workDir: workDir},
		&pythonAnalyzeTool{workDir: workDir},
		&fetchURLTool{},
	}
	return t
}

// Tools returns the underlying invokable tools.
func (t *ToolSet) Tools() []tool.InvokableTool {
	return t.tools
}

// Infos returns the schema.ToolInfo for each tool.
func (t *ToolSet) Infos(ctx context.Context) ([]*schema.ToolInfo, error) {
	infos := make([]*schema.ToolInfo, 0, len(t.tools))
	for _, tt := range t.tools {
		info, err := tt.Info(ctx)
		if err != nil {
			return nil, err
		}
		infos = append(infos, info)
	}
	return infos, nil
}

// Execute runs a single tool call and returns its result.
func (t *ToolSet) Execute(ctx context.Context, tc schema.ToolCall) (string, error) {
	for _, tt := range t.tools {
		info, err := tt.Info(ctx)
		if err != nil {
			return "", err
		}
		if info.Name == tc.Function.Name {
			return tt.InvokableRun(ctx, tc.Function.Arguments)
		}
	}
	return "", fmt.Errorf("unknown tool: %s", tc.Function.Name)
}

// sanitizePath ensures p stays within workDir. If p is absolute it must be
// under workDir; relative paths are resolved against workDir.
func sanitizePath(workDir, p string) (string, error) {
	if workDir == "" {
		return "", fmt.Errorf("working directory not set")
	}
	p = filepath.Clean(p)
	if filepath.IsAbs(p) {
		rel, err := filepath.Rel(workDir, p)
		if err != nil || strings.HasPrefix(rel, "..") {
			return "", fmt.Errorf("path %q escapes working directory", p)
		}
		return p, nil
	}
	return filepath.Join(workDir, p), nil
}

// bashTool executes a shell command inside the task working directory.
type bashTool struct {
	workDir string
}

func (t *bashTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "bash",
		Desc: "Execute a shell command in the task sandbox. Use this to run grep, find, git, Python one-liners, or build commands. Avoid destructive operations.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"command": {
				Type:     schema.String,
				Desc:     "Shell command to execute",
				Required: true,
			},
			"timeout_seconds": {
				Type:     schema.Integer,
				Desc:     "Maximum time to allow the command to run (default 60)",
				Required: false,
			},
		}),
	}, nil
}

func (t *bashTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	var args struct {
		Command        string `json:"command"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}
	args.Command = strings.TrimSpace(args.Command)
	if args.Command == "" {
		return "", fmt.Errorf("empty command")
	}
	if isForbiddenCommand(args.Command) {
		return "", fmt.Errorf("command blocked for safety: %s", args.Command)
	}

	if args.TimeoutSeconds <= 0 {
		args.TimeoutSeconds = 60
	}
	if args.TimeoutSeconds > 600 {
		args.TimeoutSeconds = 600
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(args.TimeoutSeconds)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", args.Command)
	if t.workDir != "" {
		cmd.Dir = t.workDir
	}
	cmd.Env = append(os.Environ(), "CI=true", "NONINTERACTIVE=1")

	out, err := cmd.CombinedOutput()
	result := string(out)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return result + "\n[timeout]", nil
		}
		return result + fmt.Sprintf("\n[error] %v", err), nil
	}
	return result, nil
}

func isForbiddenCommand(cmd string) bool {
	lower := strings.ToLower(cmd)
	forbidden := []string{
		"rm -rf /", "rm -rf /*", ":(){ :|:& };:", "mkfs.", "dd if=/dev/zero",
		"> /dev/sda", "mv / /dev/null", "chmod -R 777 /",
	}
	for _, f := range forbidden {
		if strings.Contains(lower, f) {
			return true
		}
	}
	return false
}

// readFileTool reads a text file inside the task working directory.
type readFileTool struct {
	workDir string
}

func (t *readFileTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "read_file",
		Desc: "Read the contents of a text file. Returns an error if the file is missing or too large. " +
			"If the file does not exist, use `glob` or `bash` to list the directory and confirm the exact name/casing. " +
			"To search for patterns across files, prefer `grep_search` over reading many files individually.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"path": {
				Type:     schema.String,
				Desc:     "Relative or absolute path to the file",
				Required: true,
			},
			"offset": {
				Type:     schema.Integer,
				Desc:     "Byte offset to start reading from (default 0)",
				Required: false,
			},
			"limit": {
				Type:     schema.Integer,
				Desc:     "Maximum bytes to read (default 262144). For files larger than this, the response includes the total size and instructions on how to read the next chunk.",
				Required: false,
			},
		}),
	}, nil
}

func (t *readFileTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	var args struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}
	path, err := sanitizePath(t.workDir, args.Path)
	if err != nil {
		return "", err
	}
	if args.Offset < 0 {
		args.Offset = 0
	}
	if args.Limit <= 0 {
		args.Limit = 256 * 1024
	}
	if args.Limit > 1024*1024 {
		args.Limit = 1024 * 1024
	}

	resolved, note, err := resolveReadablePath(path, t.workDir, args.Path)
	if err != nil {
		return "", err
	}
	path = resolved

	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", args.Path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", args.Path)
	}
	totalSize := info.Size()

	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", args.Path, err)
	}
	defer f.Close()

	if args.Offset > 0 {
		if _, err := f.Seek(int64(args.Offset), io.SeekStart); err != nil {
			return "", fmt.Errorf("seek %s: %w", args.Path, err)
		}
	}

	buf := make([]byte, args.Limit)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("read %s: %w", args.Path, err)
	}
	content := string(buf[:n])
	endOffset := args.Offset + n

	if int64(endOffset) < totalSize {
		content += fmt.Sprintf(
			"\n\n[FILE TRUNCATED] Read bytes %d-%d of %d total. "+
				"To continue reading, call read_file with path=%q, offset=%d, limit=%d.",
			args.Offset, endOffset, totalSize, args.Path, endOffset, args.Limit,
		)
	}
	if note != "" {
		content = note + "\n\n" + content
	}
	return content, nil
}

// resolveReadablePath tries the exact path first. If it is missing it performs a
// case-insensitive search for the requested basename under workDir. This handles
// the common LLM mistake of mis-casing filenames (e.g. Privileged-Exec.ts).
// It returns a human-readable note when a fuzzy match was used.
func resolveReadablePath(path, workDir, requested string) (resolved, note string, err error) {
	_, stErr := os.Stat(path)
	if stErr == nil {
		return path, "", nil
	}
	if !os.IsNotExist(stErr) {
		return "", "", fmt.Errorf("stat %s: %w", requested, stErr)
	}

	base := filepath.Base(requested)
	var matches []string
	_ = filepath.WalkDir(workDir, func(p string, d os.DirEntry, werr error) error {
		if werr != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Base(p), base) {
			rel, _ := filepath.Rel(workDir, p)
			matches = append(matches, rel)
		}
		return nil
	})

	if len(matches) == 1 {
		actual := filepath.Join(workDir, matches[0])
		return actual, fmt.Sprintf("[NOTE: path %q not found; using case-insensitive match %q]", requested, matches[0]), nil
	}
	if len(matches) > 1 {
		return "", "", fmt.Errorf("stat %s: file not found; multiple case-insensitive matches: %s", requested, strings.Join(matches, ", "))
	}

	// No basename match; give a concise directory hint if the parent exists.
	parent := filepath.Dir(path)
	if entries, rerr := os.ReadDir(parent); rerr == nil && len(entries) > 0 {
		maxHint := 20
		names := make([]string, 0, len(entries))
		for i, e := range entries {
			if i >= maxHint {
				names = append(names, "...")
				break
			}
			names = append(names, e.Name())
		}
		return "", "", fmt.Errorf("stat %s: file not found (no case-insensitive match). Sibling files in %s: %s", requested, filepath.Dir(requested), strings.Join(names, ", "))
	}

	return "", "", fmt.Errorf("stat %s: file not found (no case-insensitive match)", requested)
}

// writeFileTool writes a text file inside the task working directory.
type writeFileTool struct {
	workDir string
}

func (t *writeFileTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "write_file",
		Desc: "Write text content to a file. Creates parent directories if needed. Use for PoCs, reports, and harnesses.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"path": {
				Type:     schema.String,
				Desc:     "Relative or absolute path",
				Required: true,
			},
			"content": {
				Type:     schema.String,
				Desc:     "Text content to write",
				Required: true,
			},
		}),
	}, nil
}

func (t *writeFileTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}
	path, err := sanitizePath(t.workDir, args.Path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}
	if err := os.WriteFile(path, []byte(args.Content), 0o600); err != nil {
		return "", fmt.Errorf("write %s: %w", args.Path, err)
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(args.Content), args.Path), nil
}

// grepTool searches file contents using a regular expression.
type grepTool struct {
	workDir string
}

func (t *grepTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "grep_search",
		Desc: "Search file contents for a pattern and return matching lines with file paths. Equivalent to grep -R -n.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"pattern": {
				Type:     schema.String,
				Desc:     "Regular expression or literal string to search for",
				Required: true,
			},
			"path": {
				Type:     schema.String,
				Desc:     "Directory or file to search (default: working directory)",
				Required: false,
			},
			"max_results": {
				Type:     schema.Integer,
				Desc:     "Maximum number of matches to return (default 50)",
				Required: false,
			},
		}),
	}, nil
}

func (t *grepTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	var args struct {
		Pattern    string `json:"pattern"`
		Path       string `json:"path"`
		MaxResults int    `json:"max_results"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}
	if args.Pattern == "" {
		return "", fmt.Errorf("empty pattern")
	}
	if args.Path == "" {
		args.Path = "."
	}
	path, err := sanitizePath(t.workDir, args.Path)
	if err != nil {
		return "", err
	}
	if args.MaxResults <= 0 {
		args.MaxResults = 50
	}

	re, err := regexp.Compile(args.Pattern)
	if err != nil {
		return "", fmt.Errorf("invalid pattern: %w", err)
	}

	root := path
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", args.Path, err)
	}
	if !info.IsDir() {
		data, err := os.ReadFile(root)
		if err != nil {
			return "", err
		}
		return grepInFile(root, data, re, args.MaxResults), nil
	}

	var results []string
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		if len(results) >= args.MaxResults {
			return filepath.SkipAll
		}
		results = append(results, grepInFile(p, data, re, args.MaxResults-len(results)))
		return nil
	})
	if err != nil {
		return "", err
	}
	return strings.Join(results, ""), nil
}

func grepInFile(path string, data []byte, re *regexp.Regexp, max int) string {
	lines := strings.Split(string(data), "\n")
	var out []string
	for i, line := range lines {
		if re.MatchString(line) {
			out = append(out, fmt.Sprintf("%s:%d:%s", path, i+1, line))
			if len(out) >= max {
				break
			}
		}
	}
	return strings.Join(out, "\n")
}

// globTool finds files matching a glob pattern.
type globTool struct {
	workDir string
}

func (t *globTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "glob",
		Desc: "Find files matching a glob pattern (e.g. '*.go', '**/*.py'). Returns a list of paths.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"pattern": {
				Type:     schema.String,
				Desc:     "Glob pattern",
				Required: true,
			},
		}),
	}, nil
}

func (t *globTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	var args struct {
		Pattern string `json:"pattern"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}
	if args.Pattern == "" {
		return "", fmt.Errorf("empty pattern")
	}

	pattern := args.Pattern
	if !filepath.IsAbs(pattern) {
		pattern = filepath.Join(t.workDir, pattern)
	}

	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", fmt.Errorf("glob %s: %w", args.Pattern, err)
	}
	if len(matches) == 0 {
		return "(no matches)", nil
	}
	return strings.Join(matches, "\n"), nil
}

// fetchURLTool fetches a URL and returns the response body as text.
type fetchURLTool struct{}

func (t *fetchURLTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "fetch_url",
		Desc: "Fetch a URL and return the response body as text. Useful for reading documentation or CVE advisories.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"url": {
				Type:     schema.String,
				Desc:     "URL to fetch",
				Required: true,
			},
		}),
	}, nil
}

func (t *fetchURLTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	var args struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}
	if args.URL == "" {
		return "", fmt.Errorf("empty url")
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, args.URL, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// pythonAnalyzeTool lets a drone generate and run a short Python script to
// answer a specific code-analysis question (e.g. trace data flow, count
// callers, parse an AST, simulate input). The script is executed inside the
// task sandbox with a tight timeout.
type pythonAnalyzeTool struct {
	workDir string
}

func (t *pythonAnalyzeTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "python_analyze",
		Desc: "Generate and execute a short Python script to answer a specific local analysis question. " +
			"Useful for: parsing ASTs, counting call sites, simulating inputs, comparing strings, building small reachability checks. " +
			"Do NOT use this for network requests or subprocess calls; use bash/fetch_url for those.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"script": {
				Type:     schema.String,
				Desc:     "Full Python script source to execute",
				Required: true,
			},
			"timeout_seconds": {
				Type:     schema.Integer,
				Desc:     "Maximum time to allow the script to run (default 60, max 300)",
				Required: false,
			},
		}),
	}, nil
}

var unsafeScriptPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)import\s+(socket|urllib|requests|httpx|subprocess|paramiko|fabric)`),
	regexp.MustCompile(`(?i)os\.system|subprocess\.|os\.popen|exec\s*\(|eval\s*\(`),
}

func (t *pythonAnalyzeTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	var args struct {
		Script         string `json:"script"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}
	args.Script = strings.TrimSpace(args.Script)
	if args.Script == "" {
		return "", fmt.Errorf("empty script")
	}
	for _, re := range unsafeScriptPatterns {
		if re.MatchString(args.Script) {
			return "", fmt.Errorf("script blocked for safety: matches %s", re.String())
		}
	}

	if args.TimeoutSeconds <= 0 {
		args.TimeoutSeconds = 60
	}
	if args.TimeoutSeconds > 300 {
		args.TimeoutSeconds = 300
	}

	scriptPath := filepath.Join(t.workDir, fmt.Sprintf(".analyze_%d.py", time.Now().UnixNano()))
	if err := os.WriteFile(scriptPath, []byte(args.Script), 0o600); err != nil {
		return "", fmt.Errorf("write script: %w", err)
	}
	defer os.Remove(scriptPath)

	// Prefer `python` when available; many Windows installs only provide that.
	py := "python"
	if _, err := exec.LookPath("python"); err != nil {
		py = "python3"
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(args.TimeoutSeconds)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, py, scriptPath)
	cmd.Dir = t.workDir
	cmd.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1")

	out, err := cmd.CombinedOutput()
	result := string(out)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return result + "\n[timeout]", nil
		}
		return result + fmt.Sprintf("\n[error] %v", err), nil
	}
	return result, nil
}
