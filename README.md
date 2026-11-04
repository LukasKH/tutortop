# Tutortop

A desktop app for learning anything you can check with a program. You write your own curriculum with an AI tutor, solve tasks in a built-in editor, and a sandboxed test runner decides whether you passed. The AI never grades you.

Tutortop runs on the coding agent you already have: your own signed-in **Claude Code** or **Codex** CLI. No API keys, and nothing is sent anywhere except through that CLI.

## How it works

1. **Create a workspace** for a topic ("Go", "Python for data work"), or start from an idea on the home screen ("Build your own shell"). The tutor interviews you about why you're learning it and writes `MISSION.md`.
2. **Approve a curriculum.** The tutor proposes modules of objectives. Every objective must be provable by unit tests.
3. **Learn, then practise.** Each objective starts with a short lesson: the theory with worked examples and sources, then a quick quiz with instant feedback. Quiz results help the tutor plan but never count as progress. You can skip a lesson if you know the topic.
4. **Solve tasks.** The tutor writes each task just in time. Before you see it, the app runs a **quality gate** in a sandbox:
   - the hidden reference solution passes every test three times, in shuffled order;
   - the starter code compiles and fails;
   - at least two plausible wrong solutions ("mutants") are caught by the tests.
5. **Run** checks the visible tests. **Submit** also runs hidden edge-case tests. Progress counts only submissions that passed.
6. **Ask for help** in the chat, or use the hint ladder (nudge → pointer → snippet → reveal). Hints are recorded.
7. **Change the plan any time** ("make tasks longer", "focus on web dev"). The tutor asks a few questions and proposes a revised curriculum. Objectives you've passed keep their progress.

## Workspace files

Each workspace is a git repo of plain files in `~/Tutortop/<topic>` (choose another folder in Settings, or set `TUTORTOP_HOME`):

```
MISSION.md  NOTES.md  GLOSSARY.md  RESOURCES.md  learning-records/   written by the tutor
lessons/0001-wrapping-errors/{README.md,lesson.yaml}                theory before a task
curriculum.yaml                                                      the approved plan
tasks/0001-reverse-words/{README.md,task.yaml,starter/,tests/,work/} work/ is your code
.tutortop/                                                           app state (attempts, chat)
```

Reference solutions, hidden tests and mutants live outside the workspace (`~/.local/share/tutortop/private/`), so neither you nor the tutor sees them by accident.

## Install

Download the installer for your system from [Releases](https://github.com/LukasKH/tutortop/releases):

| System | File | Install |
|---|---|---|
| Ubuntu 24.04+, Debian 13+ | `.deb` | `sudo apt install ./tutortop_*_amd64.deb` |
| Fedora | `.rpm` | `sudo dnf install ./tutortop-*.x86_64.rpm` |
| Arch | `.pkg.tar.zst` | `sudo pacman -U tutortop-*.pkg.tar.zst` |
| Other Linux | `.AppImage` | `chmod +x` it and run it |
| Windows 10/11 | `-setup.exe` | Run it. It's unsigned, so SmartScreen asks first: *More info → Run anyway*. |
| macOS | `.dmg` | Drag to Applications. It's unsigned, so the first time macOS blocks it: open *System Settings → Privacy & Security* and click *Open Anyway*. |

Tutortop also needs two things it doesn't install for you. It shows what's missing on the home screen.

- **A sandbox:** Docker or Podman. Tutortop uses whichever is installed (Docker first), or the one in `TUTORTOP_RUNNER`. On Windows and macOS that's Docker Desktop or Podman Desktop. On Linux, Docker must work without sudo: `sudo usermod -aG docker $USER`, then log out and in. The first check for each language builds a sandbox image, which takes a few minutes.
- **A coding agent:** [Claude Code](https://docs.claude.com/en/docs/claude-code/setup) (`claude`) or [Codex](https://github.com/openai/codex) (`codex`), signed in.

Git is optional. With it, every workspace keeps a history of its changes.

## Requirements for development

- Go 1.27+, Node 20+, and the Wails v3 CLI (`wails3`)
- Docker or Podman, and Claude Code and/or Codex, as above

## Development

```sh
mise install                 # Go, Node, golangci-lint and pre-commit, pinned in mise.toml
pre-commit install           # git hooks: lint + tests before commit, Conventional Commits on the message
wails3 dev                   # run the app with hot reload
task test                    # Go tests with the race detector (sandbox tests use Docker, skip without it)
task lint                    # go vet, Staticcheck and format checks
task format                  # format Go code and imports
npm --prefix frontend run format  # format the frontend (Prettier, sorts Tailwind classes)
npm --prefix frontend run lint    # lint the frontend (Oxlint, React hooks rules)
npm --prefix frontend test        # frontend tests (Vitest)
task test:live               # talks to the real Claude Code and Codex CLIs; uses a little quota
```

`TUTORTOP_RUNNER=local` runs learner code directly on your machine with no sandbox. Use it for development only.

[docs/architecture.md](docs/architecture.md) explains how the Go backend, the frontend and Wails fit together.

### Contributing

The pre-commit hooks and GitHub Actions run:

- golangci-lint: go vet's analyzers, Staticcheck, and gofmt/goimports formatting (`.golangci.yml`).
- `go test -race -short ./internal/...`.
- A TypeScript typecheck, an Oxlint lint, a Prettier format check and the Vitest tests of the frontend, when frontend files change.

Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`, `refactor:`, `docs:`, `test:`, `build:`, `ci:`, `chore:`), checked by commitlint.
