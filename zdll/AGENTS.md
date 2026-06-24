# zdll Agent Guide

## Module

- Path: `E:\code\kimiSec\zerodayFind\zdll`
- Module: `github.com/kimisec/zdll`
- Go: 1.25.0
- Stack: Go CLI only (Wails desktop client removed)
- LLM SDK: `github.com/MoonshotAI/kimi-agent-sdk/go`

## Build Commands

```powershell
# CLI (CGO required on Windows because of go-tree-sitter)
$env:CC='C:\ProgramData\mingw64\mingw64\bin\gcc.exe'
go build ./cmd/zdll

# Tests
$env:CC='C:\ProgramData\mingw64\mingw64\bin\gcc.exe'
go test ./...

# Vet
$env:CC='C:\ProgramData\mingw64\mingw64\bin\gcc.exe'
go vet ./...

# Dependency tidy (use direct proxy for go-tree-sitter sub-packages)
$env:GOPROXY='direct'; $env:CC='C:\ProgramData\mingw64\mingw64\bin\gcc.exe'; go mod tidy
```

> On Windows the default `cl.exe` rejects tree-sitter's C flags, so set `CC` to MinGW `gcc.exe`.

## Architecture Notes

- `internal/core` owns the engine, blackboard, drones, session pool, doc-intel, critic, sector coordinator, tree-sitter scanner, and `Location`/`changed paths` context helpers. It does not depend on CLI or the Kimi SDK directly.
- `internal/llm` wraps the official Kimi Go SDK and builds agent YAML files from `.bots.md`.
- `internal/scanner` contains the OSV bridge (with auto-download), the semantic scanner, and the `git diff` helper used by `--diff-base`.
- `internal/report` renders Markdown/JSON/SARIF/DOT/GraphML and provides stable finding keys for baseline comparison.
- `internal/app` is the application service layer shared by the CLI.
- `cmd/zdll` is the CLI entrypoint.

## Key Commands

```powershell
# Set API key via environment (recommended)
$env:KIMI_API_KEY='sk-...'
$env:KIMI_BASE_URL='https://api.kimi.com/coding/v1'   # optional, default already set
$env:KIMI_MODEL_NAME='kimi-for-coding'                 # optional

# Or edit ~/.config/zdll/config.yaml after `zdll config init`

zdll --version

zdll <target>                         # bare target, same as `zdll scan <target>`
zdll scan <target>
zdll scan <target> -d .\local_workspace -w 8 -R 10 -o report.md
zdll scan <target> --diff-base main   # only scan files changed since ref
zdll resume <target>

# CI / shift-left
zdll ci <target>
zdll ci <target> -f sarif -o result.sarif.json --fail-on high
zdll ci <target> --summary-format json
zdll ci <target> --baseline baseline.txt --generate-baseline new-baseline.txt

# Config
zdll config init
zdll config validate
zdll config get llm.model
zdll config set analysis.workers 8
zdll config unset llm.model

# Reporting and workspace management
zdll report <target> --format markdown --output report.md
zdll report <target> --work-dir .\local_workspace --format sarif -o out.sarif
zdll list
zdll config init
```

- `zdll scan <target>` — start a new analysis.
- `zdll scan <target> -d <workspace> -o <report.md> --no-report` — override workspace/output.
- `zdll scan <target> --diff-base <ref>` — scope semantic/OSV scans and post-filter findings to files changed since a git ref.
- `zdll resume <target>` — resume from workspace.
- `zdll ci <target>` — unattended analysis with `auto_approve=true`; default output format is SARIF and default `--fail-on` is `high`.
- `zdll ci ... --fail-on <severity>` — exit code `2` if any finding is at or above the threshold.
- `zdll ci ... --baseline <file>` — exit code `2` if new findings appear compared to the baseline key list.
- `zdll ci ... --generate-baseline <file>` — write current finding keys to a text file after scanning.
- `zdll report <target> --format markdown|json|sarif|dot|graphml --output report.md` — render a report from a workspace.
- `zdll list` — list persisted workspaces with status and last update time.
- `zdll config init` — create default config in `~/.config/zdll/config.yaml`.
- `zdll config validate` — load config and verify that `KIMI_API_KEY` is set.

Configuration priority: command-line flags → environment variables → `~/.config/zdll/config.yaml` → defaults.

## CI Exit Codes

- `0` — success / policy pass.
- `1` — configuration or runtime error.
- `2` — policy failure (`--fail-on`) or baseline drift (`--baseline`).

## Finding Location

Findings carry an optional `Location{File, Line, Column}` populated by:

- tree-sitter semantic scanner (file, line, and column of the sink call),
- OSV dependency scanner (manifest file),
- Drone `TRACE_TARGET` parsing (best-effort file/line).

The SARIF renderer uses `Location` directly; baseline keys include the file path (and line when available) for stable cross-run comparison.
