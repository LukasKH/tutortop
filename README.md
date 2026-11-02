# Tutortop

A desktop app for learning anything you can check with a program. You write your own curriculum with an AI coach, solve tasks in a built-in editor, and a sandboxed test runner decides whether you passed. The AI never grades you.

Tutortop runs on the coding agent you already have: your own signed-in **Claude Code** or **Codex** CLI. No API keys, and nothing is sent anywhere except through that CLI.

## How it works

1. **Create a workspace** for a topic ("Go", "Python for data work"), or start from an idea on the home screen ("Build your own shell"). The coach interviews you about why you're learning it and writes `MISSION.md`.
2. **Approve a curriculum.** The coach proposes modules of objectives. Every objective must be provable by unit tests.
3. **Learn, then practise.** Each objective starts with a short lesson: the theory with worked examples and sources, then a quick quiz with instant feedback. Quiz results help the coach plan but never count as progress. You can skip a lesson if you know the topic.
4. **Solve tasks.** The coach writes each task just in time. Before you see it, the app runs a **quality gate** in a sandbox:
   - the hidden reference solution passes every test three times, in shuffled order;
   - the starter code compiles and fails;
   - at least two plausible wrong solutions ("mutants") are caught by the tests.
5. **Run** checks the visible tests. **Submit** also runs hidden edge-case tests. Progress counts only submissions that passed.
6. **Ask for help** in the chat, or use the hint ladder (nudge → pointer → snippet → reveal). Hints are recorded.
7. **Change the plan any time** ("make tasks longer", "focus on web dev"). The coach asks a few questions and proposes a revised curriculum. Objectives you've passed keep their progress.

## Workspace files

Each workspace is a git repo of plain files in `~/Tutortop/<topic>` (choose another folder in Settings, or set `TUTORTOP_HOME`):

```
MISSION.md  NOTES.md  GLOSSARY.md  RESOURCES.md  learning-records/   written by the coach
lessons/0001-wrapping-errors/{README.md,lesson.yaml}                theory before a task
curriculum.yaml                                                      the approved plan
tasks/0001-reverse-words/{README.md,task.yaml,starter/,tests/,work/} work/ is your code
.tutortop/                                                           app state (attempts, chat)
```

Reference solutions, hidden tests and mutants live outside the workspace (`~/.local/share/tutortop/private/`), so neither you nor the tutor sees them by accident.

## Requirements

- Go 1.27+, Node 20+, and the Wails v3 CLI (`wails3`)
- Docker (or Podman with `TUTORTOP_RUNNER=podman`), usable without sudo (on Linux: `sudo usermod -aG docker $USER`, then log out and in). The first check per language builds a sandbox image, which takes a few minutes.
- Claude Code (`claude`) and/or Codex (`codex`), signed in

## Development

```sh
mise install                 # Go, Node, golangci-lint and pre-commit, pinned in mise.toml
pre-commit install           # git hooks: lint + tests before commit, Conventional Commits on the message
wails3 dev                   # run the app with hot reload
task test                    # Go tests with the race detector (sandbox tests use Docker, skip without it)
task lint                    # go vet, Staticcheck and format checks
task format                  # format Go code and imports
task test:live               # talks to the real Claude Code and Codex CLIs; uses a little quota
```

`TUTORTOP_RUNNER=local` runs learner code directly on your machine with no sandbox. Use it for development only.

### Contributing

The pre-commit hooks and GitHub Actions run:

- golangci-lint: go vet's analyzers, Staticcheck, and gofmt/goimports formatting (`.golangci.yml`).
- `go test -race -short ./internal/...`.
- A TypeScript typecheck of the frontend, when frontend files change.

Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`, `refactor:`, `docs:`, `test:`, `build:`, `ci:`, `chore:`), checked by commitlint.

## Architecture

See [docs/architecture.md](docs/architecture.md) for the file layout, data on disk and UI structure, [docs/call-stacks.md](docs/call-stacks.md) for how the main flows run through the code, [docs/agents.md](docs/agents.md) for how Claude Code and Codex are driven, and [docs/design.md](docs/design.md) for the visual rules.

| Package | Role |
|---|---|
| `internal/runner` | Runs checks in a throwaway container (non-root, no network, all capabilities dropped, CPU/memory/PID limits, 20 s test timeout, 4 MB output cap), parses `go test -json` and pytest JUnit output, and implements the quality gate |
| `internal/workspace` | The workspace files, curriculum validation, tasks, attempt log and derived progress |
| `internal/agent` | Drives the CLIs: Claude Code over its stream-json protocol (as the Claude Agent SDK does), Codex over `codex app-server` JSON-RPC (as T3 Code does) |
| `internal/app` | The Wails service for the frontend, the coach prompt, and a localhost MCP server per workspace (`get_state`, `ask_user`, `propose_curriculum`, `set_icon`, `set_toolchain`; background writers, one per objective, get `create_lesson` and `create_task`) |
| `frontend/` | React + TypeScript UI (Monaco editor) |

The coach's instructions are in `internal/app/prompts/coach.md`.
