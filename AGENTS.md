# Repository Guidelines

## Project Structure & Module Organization

`kimi.py` is the main CLI entrypoint and delegates scan work to `kimi_hive.py`. Core orchestration lives in `engine/` (`blackboard`, `cerebrum`, `drone`, storage backends), while service integrations live in `server/` (`http`, `mcp`, `worker`, `core`). The public site is under `index/` with HTML, CSS, JS, assets, data, and Nginx config; `web_ui/index.html` is a separate static UI page. Domain playbooks live in `skills/`, model training utilities in `model_q/`, helper scripts in `tools/`, and attack automation in `knownAttack/`. Treat `local_workspace/`, `attack_workspace/`, `tmp/`, reports, caches, and generated blackboard/audit files as runtime artifacts.

## Build, Test, and Development Commands

- `python -m pip install -r requirements.txt`: install runtime dependencies for local CLI/server use.
- `python -m pip install -e ".[dev]"`: install editable package plus `pytest`, `black`, `ruff`, and `mypy`.
- `python kimi.py scan https://github.com/owner/repo`: run a repository scan through the main CLI.
- `python kimi_hive.py /path/to/project --workers 8 --resume`: run the hive engine directly.
- `python index/website_server.py`: serve the local public site/API from `index/`.
- `docker compose up -d`: start the Redis, worker, and server stack.

## Coding Style & Naming Conventions

Use Python 3.11+ and four-space indentation. Formatting is `black` with a 100-column line length; lint with `ruff` using the configured `E`, `F`, `W`, `I`, `N`, and `UP` rules. Keep Python modules, functions, and variables in `snake_case`; classes in `PascalCase`; skill directories in lowercase kebab-style names such as `skills/supply-chain/`. Prefer explicit async boundaries in engine/server code and avoid committing generated `__pycache__` or workspace files.

## Testing Guidelines

`pyproject.toml` configures `pytest` for `tests/` and `test_*.py` files with `pytest-asyncio` in auto mode. Add new tests under `tests/` when possible instead of expanding scratch tests under `tmp/`. Run `python -m pytest` for the suite, or target a file with `python -m pytest tests/test_example.py`. No coverage gate is currently configured, so document any untested risk in the PR.

## Commit & Pull Request Guidelines

Recent commit subjects are not descriptive, so use clear imperative subjects going forward, for example `Add Redis worker health check`. PRs should include a concise summary, linked issue or motivation, commands run, and any environment/configuration changes. Include screenshots for `index/` or `web_ui/` changes.

## Security & Configuration Tips

Never commit `.env`, API keys, HMAC secrets, browser session data, local workspaces, or generated blackboard/audit output. Start from `.env.example`, keep production secrets outside the repo, and sanitize vulnerability reports before sharing them externally.
