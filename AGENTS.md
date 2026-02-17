# Repository Guidelines

## Project Structure & Module Organization
- `main.go`: core CLI implementation (command parsing, simulator/idb calls, frame generation, JSON output).
- `go.mod`: module definition and Go toolchain baseline.
- `skills/simagent/`: local skill assets (`SKILL.md`, `references/`, `agents/openai.yaml`) used by automation agents.
- `README.md`: user-facing usage and command reference; update it when CLI flags or behavior change.
- `simagent` (binary) may exist locally as a build artifact; treat it as generated output.

## Build, Test, and Development Commands
- `go build -o simagent .`: build the CLI binary.
- `./simagent`: show top-level help and available commands.
- `./simagent frame --target booted --json`: quick integration smoke test against a booted simulator.
- `go test ./...`: run all tests (currently expected to pass even when no test files exist).
- `gofmt -w main.go`: apply required formatting before commit.

## Coding Style & Naming Conventions
- Follow standard Go style and always run `gofmt` (tabs/spacing handled automatically).
- Prefer small, single-purpose functions and explicit error returns over hidden fallback logic.
- Use `camelCase` for local identifiers, `PascalCase` for exported names, and uppercase only for stable constants (for example, error codes).
- Keep CLI command and flag names lowercase and descriptive (for example, `ui tap`, `--target`, `--json`).

## Testing Guidelines
- Add tests as `*_test.go` files near the behavior being changed.
- Prefer table-driven tests for parsing, normalization, and error-shape logic.
- For simulator-dependent changes, keep unit coverage for deterministic logic and include manual verification commands in PR notes.

## Commit & Pull Request Guidelines
- Use concise, imperative commit subjects consistent with current history (for example, `Refactor code structure...`, `Add simagent skill documentation...`).
- Keep each commit focused on one change theme.
- PRs should include: behavior summary, verification evidence (`go test ./...` and any manual `./simagent ...` checks), and screenshots/artifacts when frame annotation output changes.

## Agent-Specific Instructions
- If `.agent/PLANS.md` exists and you are doing a large feature or refactor, create an `ExecPlan` first and follow it before implementation.
