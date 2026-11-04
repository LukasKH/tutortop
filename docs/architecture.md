# Architecture

Tutortop is a Go program with a React frontend, shipped as one desktop app by [Wails v3](https://wails.io). Go does the real work. It reads and writes the workspace files, runs the learner's coding agent and runs checks in a sandbox. The frontend draws the UI and asks Go for everything else.

```
 React frontend (frontend/src)
   |  calls App methods            ^  events (chat:message, thread:busy, ...)
   v  (generated bindings)         |
 internal/app  App, the one Wails service
   |-- internal/workspace   files on disk, one git repo per topic
   |-- internal/agent       the learner's Claude Code or Codex CLI
   |-- internal/runner      Docker/Podman sandbox: checks, quality gate
   '-- MCP server on 127.0.0.1, the tools the agent works through
```

## Packages

- `main.go` gives the app the user's shell PATH, builds `App`, registers it with Wails and opens the window.
- `internal/app` holds `App`. Every exported method on it is callable from the frontend. It also runs the chat sessions, the background writers, the MCP server and the language servers.
- `internal/workspace` stores one topic as plain files in `~/Tutortop/<id>`, plus private files (hidden tests, reference solutions) under the XDG data dir. It knows nothing about agents or Wails.
- `internal/agent` starts the Claude Code or Codex CLI, streams its output as text and tool events, and decides which tool calls to allow. The agent may change files in the workspace, except under `tasks/`, and may not run commands.
- `internal/runner` runs tests in a Docker or Podman container built for the task's toolchain. It also runs the quality gate for new tasks.
- `frontend/src/lib/api.ts` wraps the generated bindings and events; components import from it. A few modules use the Wails runtime directly: links open in the browser, the settings pick a folder, the window zooms, and the language client listens to its event outside React. The frontend's own layout is described under [Frontend](#frontend).

## How Wails fits in

Wails links the two halves in two directions. The frontend calls Go methods and gets an answer. Go sends events when something happens that the frontend didn't ask about.

### Calls from the frontend

`main.go` registers `App` as the app's only service. `wails3 generate bindings` reads it and writes `frontend/bindings`, with one TypeScript function per exported method and a TypeScript type for every struct those methods take or return. `lib/api.ts` re-exports them as `App`, so a component writes `App.RunChecks(wsID, taskID, files, false)`. A returned Go error becomes a rejected promise.

The bindings are generated, and committed. After changing an exported method or a type it returns, run `wails3 generate bindings -clean=true -ts -i` and commit the result. CI regenerates them and fails if they differ.

### Events from Go

Some things start on the Go side. The agent streams a reply, a writer publishes a task, a sandbox check finishes, a language server answers. Go emits an event for each one, and the frontend listens.

All events live in `internal/app/events.go`. Each has a payload struct, a name constant in `namespace:name` form whose type names the payload (`chatMessage event[ChatEvent] = "chat:message"`), and an `application.RegisterEvent[Payload](string(name))` call in `init()`. Registering does two things. The binding generator writes the names to `frontend/bindings/events` and types each event's data in TypeScript, and Wails drops an event whose data has the wrong type. It only logs that, and release builds discard the log, so a mistake fails silently: `TestEventsRegistered` checks that every name constant is registered with its payload type.

| Event | Payload | Sent when |
| --- | --- | --- |
| `chat:message` | `ChatEvent` | a chat message is added or updated, including each streamed piece of a reply |
| `thread:busy` | `BusyEvent` | a thread's tutor or writer starts or stops working |
| `gate:status` | `GateEvent` | a new task or toolchain is being checked in the sandbox, or the check finished |
| `task:published` | `TaskEvent` | a writer published a task |
| `lesson:published` | `LessonEvent` | a writer published a lesson |
| `questions:asked` | `QuestionsEvent` | the tutor called `ask_user` |
| `workspace:changed` | `WorkspaceEvent` | anything else in the workspace changed; the frontend reloads it |
| `curriculum:proposed` | `WorkspaceEvent` | the tutor proposed a curriculum |
| `lsp:message` | `LSPEvent` | a language server sent a message, or stopped |

Go code sends with `emit(name, payload)`. Because the name's type carries the payload type, `emit(chatMessage, BusyEvent{...})` doesn't compile. The compiler can't link the `RegisterEvent` calls to the names, because the binding generator needs them written out literally, so `TestEventsRegistered` checks those. `emit` looks up the running Wails app with `application.Get()`. Tests run without one, and there `emit` does nothing, so tests check what changed through `App` methods.

In the frontend, `useEvent(AppEvents.ChatMessage, (ev) => ...)` listens for as long as the component is mounted. Most payloads carry the workspace id, and views ignore events for other workspaces.

To add an event, add the payload struct, the constant and the `RegisterEvent` line to `events.go`, call `emit` where it happens, then regenerate the bindings.

### Server mode

Built with `-tags server`, the same binary serves the frontend at `http://localhost:8080` with no window. Calls go over `POST /wails/runtime` and events over a WebSocket at `/wails/events`. This is handy for previewing the UI in a browser. Point `TUTORTOP_HOME` and `XDG_DATA_HOME` at scratch folders so a preview never touches real workspaces.

## Frontend

React 19 with Vite, styled with [Tailwind CSS v4](https://tailwindcss.com) and [shadcn/ui](https://ui.shadcn.com) components. Files use kebab-case names, and each file puts its exported components first.

```
frontend/src/
  main.tsx, app.tsx     entry point and the top-level screen switch
  features/<area>/      one folder per screen area: home, settings, workspace, lesson, task, tutor
  components/ui/        shadcn primitives (Button, Dialog, Tabs, ...), restyled to the app's look
  components/           the app's shared components: Markdown, Page and PageHeader, Splitter, ...
  hooks/                shared React hooks
  lib/                  api.ts (the Wails bindings and events), thread names, chat helpers, wording
  styles/               globals.css (theme and base), prose.css (rendered markdown)
```

The theme lives in `styles/globals.css`. Colours are CSS variables named the shadcn way (`--background`, `--foreground`, `--muted-foreground`, `--border`, ...), plus Tutortop's own `--soft-foreground`, `--faint`, `--pass` and `--fail`. Use them through Tailwind classes such as `text-muted-foreground` or `text-pass`, not raw colours. Colour is only for check results, green passed and red failed.

A feature's state and its backend calls live in hooks next to its components. The components render what the hooks return. Pages that own their data load it themselves: a lesson (`LessonPanel`), an exercise with autosave and checks (`features/task/use-task-work.ts`), Home and Settings call `App` in an effect and keep their own edits.

### The workspace screen

`features/workspace/workspace-view.tsx` lays out the curriculum rail, the centre pane and the tutor panel. Its state comes from these hooks:

| Hook | State | Comes from |
| --- | --- | --- |
| `use-workspace.ts` | the snapshot (`App.Workspace`), busy threads, writers' checks and failures, the planning check, open questions, the last error | refetched on `workspace:changed`, `task:published` and `lesson:published`; `thread:busy`, `gate:status`, `chat:message` and `questions:asked` update it directly |
| `use-open-item.ts` | the open item, the centre's view (item, plan or proposal), the chat thread that goes with them, new work to point out | the learner, remembered per workspace; `task:published`, `lesson:published` and `curriculum:proposed` |
| `use-chat-threads.ts` | conversations by thread, and which are unread | `App.Chat` for the shown thread; `chat:message` |
| `use-chat-panel.ts`, `tutor/use-composer.ts` | the tutor panel's width and whether it's folded; the message draft | the learner |

`use-workspace.ts` sends every change through one pure reducer, `workspace-state.ts`. Events arrive faster than refetches, so the reducer keeps what an event set after a snapshot was asked for. A writer that finishes during a refetch then stops spinning.

The open item is an `OpenItem`: a task, a lesson, or an objective's lesson or exercise that isn't written yet (`open-item.ts`). `centerOf` in `use-open-item.ts` decides what the centre shows, and `CenterPane` renders it. Thread names (`plan`, a task id, `lesson-<id>`, `write-<objective>`) are the backend's and live in `lib/workspace.ts`. Only the hooks and the chat pass them around.

To add:

- **A centre page:** add a kind to `Center`, decide when it shows in `centerOf`, and render it in `CenterPane`. The compiler points at the missing `case`.
- **An event:** after adding it in Go and regenerating the bindings, listen with `useEvent` in the hook that owns the state it changes. For the workspace's state, add an `Action` and its case in `reduceWorkspace`.
- **A backend call:** call `App.<Method>` from the hook or component that owns the state. Add read-only methods to `SAFE_TO_REPEAT` in `lib/api.ts` so they wait out a restart. In the workspace screen, pass failures to `fail`, which shows them above the centre pane.

### Tests

`npm test` runs [Vitest](https://vitest.dev). Tests sit next to the module they test (`*.test.ts`) and cover logic that can break: the workspace reducer, open-item rules, chat merging, and hooks that react to events. Hook tests render with `@testing-library/react` in jsdom (`// @vitest-environment jsdom`) and replace `useEvent` with a fake to send events; `use-open-item.test.ts` shows how. `features/workspace/fixtures.ts` builds workspace snapshots.

Components are styled with utility classes in their markup. The one exception is rendered markdown (lessons, task statements, tutor replies), which `prose.css` styles by element because `Markdown` renders plain HTML. Add shadcn components with `npx shadcn@latest add <name>`; they land in `components/ui/`.

## The agent and its tools

The tutor is the learner's own CLI, signed in already. `App` starts one agent process per chat thread. The threads are `plan` for the mission and curriculum, one per task, one per lesson, and one writer per objective (`write-<objective>`).

Writers have no chat window. They prepare an objective's lesson and exercise in the background, and the app keeps the next two unpassed objectives written so the learner doesn't wait.

The agent changes the app's state only through MCP tools. `App` serves them over HTTP on `127.0.0.1`, with a random token in each thread's URL, so a thread can only reach its own workspace. The tool handlers write to the workspace and emit events.

- Every thread gets `get_state`.
- Chat threads also get `ask_user`, `set_icon`, `propose_curriculum` and `set_toolchain`.
- Writers also get `create_task` and `create_lesson`.

`create_task` publishes a task only after it passes the quality gate in the sandbox. The reference solution has to pass three times in shuffled order, the starter code has to fail, and the tests have to catch at least two wrong solutions.

## Checks and the editor

**Run** and **Submit** call `App.RunChecks`. It saves the learner's code, runs the visible tests (plus the hidden ones on submit) in the sandbox and records the attempt.

The editor's completions and errors come from a language server running in the task's sandbox image. Go doesn't read the LSP messages. It relays them between the server's stdio and the frontend, sending them out as `lsp:message` events and taking them in through `App.SendLanguageServer`.
