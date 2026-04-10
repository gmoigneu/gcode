# gcode project instructions

Go-based coding agent. Go 1.24+. See `specs/` for detailed phase specs and `specs/backlog.md` for future work.

## Development workflow

Every task follows the same process. No exceptions.

### 1. Pick an issue

If the user provides an issue number, use it. If not, pick the next open issue that makes sense: complete the current milestone (phase) before moving to the next. Phases are sequential, issues within a phase can be done in dependency order.

```bash
gh issue list --milestone "Phase N: ..." --state open
```

### 2. Create a branch

Branch from `main`. Include the issue number.

```bash
git checkout main && git pull
git checkout -b feat/<issue-number>-<short-description>
```

### 3. Implement

- Use model `gateway-opus-46` for all implementation work
- Read the linked spec section before writing any code
- Write tests first (TDD): failing test, then minimal code to pass
- Run tests after every change: `go test ./pkg/<package>/...`
- Run full suite before committing: `go test ./...`

### 4. Commit and push

Use conventional commits. Reference the issue number.

```bash
git add -A
git commit -m "feat(scope): description, closes #N"
git push -u origin feat/<issue-number>-<short-description>
```

### 5. Create a draft PR

```bash
gh pr create --draft \
  --title "feat(scope): description" \
  --body "Closes #N

## What
<what the change does>

## How to test
<test commands>
"
```

### 6. Code review

Dispatch a code review subagent using model `gateway-gpt-54`. The reviewer checks:

- Correctness against the spec
- Test coverage
- Error handling
- No goroutine leaks
- No unintended side effects

```
Use the code-review-checklist skill.
Review the diff for PR #N against specs/go-agent-phase<X>.md.
```

The reviewer posts findings as a PR comment.

### 7. Address review feedback

Back on `gateway-opus-46`: read the review comment, fix issues, commit with:

```bash
git add -A
git commit -m "fix(scope): address review feedback, refs #N"
git push
```

### 8. Final check and merge

On `gateway-opus-46`: run the full test suite one more time.

```bash
go test ./...
go vet ./...
```

If everything passes, mark PR as ready and merge:

```bash
gh pr ready
gh pr merge --squash --delete-branch
```

### 9. Clean up

```bash
git checkout main
git pull
```

### Summary of the flow

```
issue → branch → implement (gateway-opus-46) → commit → draft PR
  → review (gateway-gpt-54) → fix feedback → final check (gateway-opus-46)
  → merge → checkout main → pull
```

## Models

| Task | Model |
|---|---|
| Implementation | `gateway-opus-46` |
| Code review | `gateway-gpt-54` |

## Project structure

```
cmd/gcode/          CLI entry point
pkg/ai/             LLM streaming abstraction
pkg/agent/          Agent runtime
pkg/tools/          Built-in tools (read, bash, edit, write, ask_user, fetch)
pkg/store/          SQLite session persistence
pkg/compaction/     Context window management
pkg/tui/            Terminal UI framework
pkg/plugin/         Skills and subprocess plugins
internal/prompt/    System prompt builder
internal/config/    Settings, auth, model registry
specs/              Phase specs and backlog
```

## Build and test

```bash
go build ./...           # build everything
go test ./...            # run all tests
go test ./pkg/ai/...     # run package tests
go vet ./...             # static analysis
```

Integration tests (require API keys) use build tags:

```bash
go test -tags integration ./pkg/ai/...
```

## Conventions

- Go 1.24+. Use generics and range-over-func where appropriate.
- Raw HTTP for all LLM providers. No vendor SDKs.
- SQLite via `modernc.org/sqlite` (pure Go, no CGo).
- Custom TUI from scratch. No Bubble Tea, no tcell.
- Errors as values, not panics. Errors in streams are events on the channel.
- Use `context.Context` for cancellation, not custom signal types.
- `json.RawMessage` for deferred deserialization (message payloads in session entries).
- Test with `t.TempDir()` for filesystem tests, `:memory:` for SQLite tests.

## Phase order

Complete each phase fully before starting the next. A phase is complete when all its milestone issues are closed and tests pass.

1. `pkg/ai` (LLM streaming, all providers)
2. `pkg/agent` (agent loop, liveness events)
3. `pkg/tools` (read, bash, edit, write, ask_user, fetch)
4. `pkg/store` (SQLite session persistence)
5. `pkg/compaction` (context window management)
6. `pkg/tui` (terminal UI)
7. `cmd/gcode` (CLI wiring)
8. `pkg/plugin` (skills, subprocess plugins)
9. Integration (system prompt, settings, AGENTS.md loading, polish)

## Config loading

gcode loads configuration from multiple sources (highest precedence first):

1. `.gcode/` (project-level gcode config)
2. `.agents/` (project-level cross-agent config)
3. `~/.gcode/` (global gcode config)
4. `~/.agents/` (global cross-agent config)

This applies to AGENTS.md files, skills, settings, and auth.
