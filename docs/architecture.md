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
- `internal/agent` starts the Claude Code or Codex CLI, streams its output as text and tool events, and decides which tool calls to allow. The agent may change files in the workspace, except under `tasks/`, and may not run commands. [Adding an agent](agents.md) describes its contract.
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
| `check:status` | `CheckEvent` | a sandbox check (`SandboxCheck`) started or finished: a writer's new task in the quality gate, or a language a chat sets up |
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
  features/<area>/      one folder per screen area: home, settings, workspace, lesson, task, chat
  components/ui/        shadcn primitives (Button, Dialog, Tabs, ...), restyled to the app's look
  components/           the app's shared components: Markdown, Page and PageHeader, Splitter, ...
  hooks/                shared React hooks
  lib/                  api.ts (the Wails bindings and events), agents and models, thread names, chat helpers, wording
  styles/               globals.css (theme and base), prose.css (rendered markdown)
```

The theme lives in `styles/globals.css`. Colours are CSS variables named the shadcn way (`--background`, `--foreground`, `--muted-foreground`, `--border`, ...), plus Tutortop's own `--soft-foreground`, `--faint`, `--pass` and `--fail`. Use them through Tailwind classes such as `text-muted-foreground` or `text-pass`, not raw colours. Colour is only for check results, green passed and red failed.

A feature's state and its backend calls live in hooks next to its components. The components render what the hooks return. Pages that own their data load it themselves: a lesson (`LessonPanel`), an exercise with autosave and checks (`features/task/use-task-work.ts`), Home and Settings call `App` in an effect and keep their own edits.

### The workspace screen

`features/workspace/workspace-view.tsx` lays out the curriculum rail, the centre pane and the chat panel (labelled "Tutor"). Its state comes from these hooks:

| Hook | State | Comes from |
| --- | --- | --- |
| `use-workspace.ts` | the snapshot (`App.Workspace`), busy threads, writers' checks and failures, a chat's toolchain check, open questions, the last error | refetched on `workspace:changed`, `task:published` and `lesson:published`; `thread:busy`, `check:status`, `chat:message` and `questions:asked` update it directly |
| `use-open-item.ts` | the open item, the centre's view (item, plan or proposal), the chat thread that goes with them, new work to point out | the learner, remembered per workspace; `task:published`, `lesson:published` and `curriculum:proposed` |
| `use-chat-threads.ts` | conversations by thread, and which are unread | `App.Chat` for the shown thread; `chat:message` |
| `use-chat-panel.ts`, `chat/use-composer.ts` | the chat panel's width and whether it's folded; the message draft | the learner |

`use-workspace.ts` sends every change through one pure reducer, `workspace-state.ts`. Events arrive faster than refetches, so the reducer keeps what an event set after a snapshot was asked for. A writer that finishes during a refetch then stops spinning.

The open item is an `OpenItem`: a task, a lesson, or an objective's lesson or exercise that isn't written yet (`open-item.ts`). `centerOf` in `use-open-item.ts` decides what the centre shows, and `CenterPane` renders it. Thread names (`plan`, a task id, `lesson-<id>`, `write-<objective>`) are the backend's and live in `lib/workspace.ts`. Only the hooks and the chat pass them around.

To add:

- **A centre page:**
  1. Get its data. `WorkspaceState` carries only the mission of the tutor's documents, so a page for `NOTES.md` or `GLOSSARY.md` needs a field on it in Go (`App.Workspace`) or its own `App` method that the page calls itself, like `LessonPanel`. Either needs the bindings regenerated.
  2. If the learner chooses the page, as they choose "Your plan", add a value to `CenterView` in `use-open-item.ts` and a link in `Sidebar`, whose callback in `workspace-view.tsx` calls `nav.setView` (see `onShowPlan` and `planOpen`). The chat shows the plan thread for every view but `item`.
  3. Add a kind to `Center` and decide in `centerOf` when it shows. Until the first plan is approved, `centerOf` returns early with `setup`, `proposal` or `plan`; add a case there if the page is needed then too.
  4. Render it in `CenterPane`. The compiler points at the missing `case`. Add a `centerOf` case to `use-open-item.test.ts`.
- **An event:** after adding it in Go and regenerating the bindings, listen with `useEvent(AppEvents.<Name>, ...)` in the hook that owns the state it changes, and ignore other workspaces' events. In the workspace screen, an event that changes the snapshot only calls `refresh`, like `workspace:changed`. One that updates state directly adds a field to `LiveWorkspace`, an `Action` and its case in `reduceWorkspace`, with a test in `workspace-state.test.ts`. If the snapshot also carries that state, stamp the action with `performance.now()`, record when it changed (like `busyAt`) and merge the snapshot's value with `newer`, as `busy` does, so an older snapshot doesn't undo it.
- **A backend call:** after adding the Go method and regenerating the bindings, `App.<Method>` exists with its types. Re-export any new Go type it takes or returns from `lib/api.ts`, under its Go name. Call it from the hook or component that owns the state. Add read-only methods to `SAFE_TO_REPEAT` in `lib/api.ts` so they wait out a restart. In the workspace screen, pass failures to `fail`, which shows them above the centre pane. If the call changes what the snapshot shows, have Go emit `workspace:changed` or call `refresh` after it, as the hint button does.

### Tests

`npm test` runs [Vitest](https://vitest.dev). Tests sit next to the module they test (`*.test.ts`) and cover logic that can break: the workspace reducer, open-item rules, chat merging and when the tutor shows as thinking, and hooks that react to events. Hook tests render with `@testing-library/react` in jsdom (`// @vitest-environment jsdom`) and replace `useEvent` with a fake to send events; `use-open-item.test.ts` shows how. `features/workspace/fixtures.ts` builds workspace snapshots.

Components are styled with utility classes in their markup. The one exception is rendered markdown (lessons, task statements, tutor replies), which `prose.css` styles by element because `Markdown` renders plain HTML. Add shadcn components with `npx shadcn@latest add <name>`; they land in `components/ui/`.

## The agent and its tools

The agent is the learner's own CLI, signed in already. In a chat thread it's the tutor; in a writer's thread, it's that writer. `App` starts one agent process per thread, a conversation with its own chat. `workspace.Thread` names the four kinds, and its name is the chat's file name and what the frontend sends: `plan` for the mission and curriculum, the task id for a task, `lesson-<id>` for a lesson, and `write-<objective>` for an objective's writer.

Writers have no chat window. They prepare an objective's lesson and exercise in the background, and the app keeps the next two unpassed objectives written so the learner doesn't wait.

### Sessions

A thread's agent process runs in a session (`internal/app/session.go`), which answers the thread's messages one turn at a time, in order. When the learner sends a message, `SendMessage` adds a `[context]` block about the open task or lesson (`prompts.go`), posts the message to the chat and queues it with `enqueue`. That starts the session if the thread has none. The session's loop hands each message to the agent, and its `transcript` streams the reply into the chat as `chat:message` events.

- A session starts with the first message for its thread, in the background. Chats resume their saved conversation; a writer starts fresh.
- The thread is busy (`thread:busy`) from the moment a message is queued until every queued message is answered, including while the process starts. The session's count of pending messages is the only source of this state.
- When the settings change, an idle session restarts with the next message. A busy one finishes its queue first.
- A session ends when its process exits, its workspace is deleted or the app quits. A writer also ends as soon as it's idle, and starting any thread but the plan ends the workspace's idle task and lesson sessions.

The calls that message an agent (`SendMessage`, `AnswerQuestions`, `RequestHint`, `PrepareObjective`) return once the message is queued. They don't wait for the process to start. Their error means the message wasn't sent at all, for example because the thread name is invalid. Once a message is in the chat, anything that goes wrong with it is posted in that thread as a notice with status `failed`, and nowhere else:

- If the agent doesn't start (the CLI is missing or not signed in), the session posts why once, however many messages were waiting. It drops them and ends, so the thread turns idle and the next message tries a fresh start.
- If a turn fails, its error is posted and the session takes the next message. If the process exits, its error is posted, the queued messages are dropped and the next message starts a new process.
- A writer has no chat, so the objective's page shows its failure.

To add a kind of thread, add it and its name to `internal/workspace/thread.go`, give it a role in `prompts.go` (and `prompts/`), decide in `session.go` whether it resumes and when it ends, and add the kind to the tools it gets in `mcp.go`'s `mcpTools`.

The agent changes the app's state only through MCP tools, built on the [official Go SDK](https://github.com/modelcontextprotocol/go-sdk). `App` serves them over HTTP on `127.0.0.1`, with a random token in each thread's URL, so a thread can only reach its own workspace. The tool handlers write to the workspace and emit events.

`mcpTools` in `internal/app/mcp.go` lists every tool with the kinds of thread that get it:

- Every kind gets `get_state`.
- The chats (plan, task and lesson threads) also get `ask_user`, `set_icon`, `propose_curriculum` and `set_toolchain` (`mcp_chat.go`).
- Writers also get `create_task` and `create_lesson` (`mcp_writer.go`), for their own objective only.

Each tool is a `*mcp.Tool` with its name and description, a handler method on `threadTools`, and its input and output types, kept together. The SDK derives the input schema from the input type's `json` and `jsonschema` tags and rejects calls that don't match it. A handler that returns an error sends the error's text back to the agent, which fixes its call and tries again. To add a tool, write those pieces next to the others and add a line to `mcpTools` with the kinds that get it. A tool for the plan only is a line with `workspace.PlanKind`.

`create_task` publishes a task only after it passes the quality gate in the sandbox. The reference solution has to pass three times in shuffled order, the starter code has to fail, and the tests have to catch at least two wrong solutions.

## Checks and the editor

**Run** and **Submit** call `App.RunChecks`. It saves the learner's code, runs the visible tests (plus the hidden ones on submit) in the sandbox and records the attempt. The sandbox images are built ahead, in the background, so the first check is fast: when the app starts (`ServiceStartup`, for every workspace's plan language) and when the learner approves a plan. Reading a workspace never starts a build, since the frontend repeats reads freely.

The editor's completions and errors come from a language server running in the task's sandbox image. Go doesn't read the LSP messages. It relays them between the server's stdio and the frontend, sending them out as `lsp:message` events and taking them in through `App.SendLanguageServer`.
