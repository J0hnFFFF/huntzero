---
name: kimisec-hive
description: >
  Autonomous distributed security analysis powered by kimiSec Hive-Mind Engine.
  Uses first-principles reasoning and parallel drone workers to discover
  logical contradictions, state anomalies, and unbound resource access in
  any target codebase or service — without predefined vulnerability checklists.
version: "7.1.0"
author: kimiSec Team
tools:
  - start_analysis
  - get_status
  - get_findings
  - stop_analysis
mcp_server:
  command: python
  args: ["{{SKILL_DIR}}/../../kimi_mcp_server.py"]
  env:
    KIMI_API_KEY: "{{env.KIMI_API_KEY}}"
---

# kimiSec Hive-Mind — Security Analysis Engine

## What This Skill Does

This skill gives you access to **kimiSec**, an autonomous AI security analysis engine.
It does NOT use predefined checklists. Instead, it reasons from first principles to find:

- **Logical Contradictions** — The system claims property X, but code shows ¬X
- **State Anomalies** — Valid operation sequences reach undefined/unintended states
- **Unbound Resource Access** — A principal can reference resources beyond declared scope

## When To Use This Skill

Use `start_analysis` when:
- The user asks to audit a codebase, repository, or project
- You need deep, autonomous security analysis of unfamiliar code
- You want parallel hypothesis testing rather than sequential checklist scanning

## Tool Reference

### `start_analysis(target, ...)`

Launches autonomous Hive-Mind analysis. Returns immediately; analysis runs in background.

**Arguments:**
- `target` (required): GitHub URL or absolute local path
- `max_workers` (default: 5): Concurrent drone workers
- `max_rounds` (default: 30): Max analysis rounds before forced termination
- `max_tasks` (default: 200): Max drone tasks before termination
- `max_wall_time` (default: 7200): Max run time in seconds
- `stagnation_rounds` (default: 3): Stop after N rounds with no new findings

**Examples:**
```
start_analysis("https://github.com/owner/repo")
start_analysis("/home/user/myproject", max_workers=8, max_rounds=50)
start_analysis("https://github.com/openclaw/core", max_wall_time=3600)
```

### `get_status()`

Returns current analysis progress: hypothesis tree, task statistics, findings count.
Call this every 30-60 seconds to monitor a running analysis.

### `get_findings()`

Returns all confirmed security findings with severity, description, and evidence.
Can be called during or after analysis.

**Severity levels:** `critical` → `high` → `medium` → `low`

### `stop_analysis()`

Gracefully stops the running analysis. All findings discovered so far are preserved.

## Recommended Workflow

```
1. User asks: "Audit https://github.com/example/app for security issues"

2. You call:
   start_analysis("https://github.com/example/app", max_workers=5)

3. Tell the user:
   "Analysis started. The Hive-Mind engine is now autonomously exploring the codebase.
    I'll check back in a minute."

4. After ~60 seconds, call get_status() and report progress to user.

5. When analysis completes (status.active = false), call get_findings() and
   present the findings in a structured report.
```

## Understanding the Output

- **Hypotheses**: Testable claims about potential vulnerabilities (pending/active/confirmed/discarded)
- **Drones**: Parallel workers executing specific micro-tasks (topology-mapper, data-flow-tracer, etc.)
- **Findings**: Confirmed vulnerabilities with evidence

## Important Notes

- Analysis is fully autonomous — no manual intervention required unless arbitration is requested
- The engine deliberately avoids predefined vulnerability categories to encourage novel discovery
- For large codebases, increase `max_rounds` and `max_tasks` accordingly
- `stagnation_rounds=3` means: if 3 consecutive rounds produce nothing new, analysis ends
