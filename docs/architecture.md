# Architecture

Tutortop is a Go program with a React frontend, shipped as one desktop app by [Wails v3](https://wails.io). The [glossary](GLOSSARY.md) defines the words used here and in the code. Go does the real work. It reads and writes the workspace files, runs the learner's coding agent and runs checks in a sandbox. The frontend draws the UI and asks Go for everything else.

```
 React frontend (frontend/src)
   |  calls service methods        ^  events (chat:message, thread:busy, ...)
   v  (generated bindings)         |
 internal/app            wiring, and the Workspace, Task and Lesson services
   |-- internal/agent/threads   each thread's running agent, the writers, the chats (Chat service)
   |     '-- internal/agent     the learner's Claude Code or Codex CLI
   |-- internal/mcp             MCP server on 127.0.0.1, the tools the agent works through
   |-- internal/editor          the editor's language servers (Editor service)
   |-- internal/settings        settings.yaml (Settings service)
   |-- internal/sandbox         Docker/Podman sandbox: checks, quality gate, language servers
   |-- internal/workspace       files on disk, one git repo per topic
   '-- internal/events          the events Go sends the frontend
```

## Packages

Each package is named after the part of the app it represents, so a name says where it comes from: `settings.NewStore`, `threads.Agents`, `mcp.Server`.

- `main.go` gives the app the user's shell PATH, builds the services with `app.New`, registers each with Wails and opens the window.
- `internal/app` wires the app together: `New` builds the pieces the services share and the services on them. It also holds the services for the workspaces, tasks and lessons (see [Calls from the frontend](#calls-from-the-frontend)). Nothing imports it.
- `internal/agent/threads` runs each thread's agent (the chats' tutors and the background writers) as `threads.Agents` and `threads.Writers`, turns their replies into chat transcripts, holds the prompts that tell them what to do (`prompts/*.md`), and serves the chats (`threads.ChatService`).
- `internal/agent` starts the Claude Code or Codex CLI, streams its output as text and tool events, and decides which tool calls to allow. The agent may change files in the workspace, except under `tasks/`, and may not run commands. [Adding an agent](agents.md) describes its contract.
- `internal/mcp` serves the MCP tools agents change the app's state through, one server per thread, and checks the links they cite. It doesn't import the agents: it shows sandbox checks through `mcp.Checks`, which `threads.Agents` implements.
- `internal/editor` serves the editor's language servers (`editor.Service`) and relays their messages.
- `internal/settings` keeps the app-wide settings in `settings.yaml` (`settings.Store`) and serves the Settings screen (`settings.Service`).
- `internal/sandbox` runs tests in a Docker or Podman container built for the task's toolchain. It also runs the quality gate for new tasks and the language servers' processes.
- `internal/workspace` stores one topic as plain files in `~/Tutortop/<id>`, plus private files (hidden tests, reference solutions) under the XDG data dir. It knows nothing about agents or Wails.
- `internal/events` holds every event Go sends the frontend (see [Events from Go](#events-from-go)).
- `internal/decode` turns JSON, YAML and XML into typed values (`decode.JSON[T]`, ...), for every package. `internal/text` and `internal/shellpath` are small helpers.
- `frontend/src/lib/api.ts` wraps the generated bindings and events; components import from it. A few modules use the Wails runtime directly: links open in the browser, the settings pick a folder, the window zooms, and the language client listens to its event outside React. The frontend's own layout is described under [Frontend](#frontend).

Imports point one way: `app` imports the packages below it; `agent/threads` imports `agent`, `mcp`, `settings`, `sandbox`, `workspace` and `events`; `mcp` and `editor` import only `sandbox`, `workspace` and `events`. Test doubles that several packages' tests share live in `mcp/mcptest` (fake links, a tool caller, a Go task that passes the gate) and `agent/threads/threadstest` (a fake agent CLI).

## How Wails fits in

Wails links the two halves in two directions. The frontend calls Go methods and gets an answer. Go sends events when something happens that the frontend didn't ask about.

### Calls from the frontend

The backend is a set of [Wails services](https://v3.wails.io/features/bindings/services), one per area. Each is a struct whose exported methods are the frontend's calls, and gets what it uses through its constructor:

| Service (frontend name) | Go | Calls | Owns |
| --- | --- | --- | --- |
| `WorkspaceService` | `app.WorkspaceService` (`internal/app/workspaces.go`) | `ListWorkspaces`, `Activity`, `CreateWorkspace`, `DeleteWorkspace`, `Workspace`, `ApproveProposal`, `RejectProposal`, `SaveMission`, `WriteObjectiveNow` | building the plans' sandbox images ahead (`ServiceStartup`) |
| `TaskService` | `app.TaskService` (`internal/app/tasks.go`) | `Task`, `SaveWork`, `ResetWork`, `RunTests` | |
| `LessonService` | `app.LessonService` (`internal/app/lessons.go`) | `Lesson`, `AnswerQuiz` | |
| `ChatService` | `threads.ChatService` (`internal/agent/threads/chat.go`) | `Chat`, `SendMessage`, `AnswerQuestions`, `DismissQuestions`, `RequestHint`, `StopAgent` | ending the thread agents as the app quits (`ServiceShutdown`) |
| `SettingsService` | `settings.Service` (`internal/settings/service.go`) | `Settings`, `SaveSettings`, `Models`, `Environment` | |
| `EditorService` | `editor.Service` (`internal/editor/editor.go`) | `StartLanguageServer`, `SendLanguageServer`, `StopLanguageServer` | stopping the language servers as the app quits (`ServiceShutdown`) |

`app.New` reads the settings, opens the workspaces folder, starts the MCP server (`mcp.Start`) and the thread agents (`threads.NewAgents`), and hands them to `newServices`. That builds the rest of the pieces several services share once (`threads.Writers`, `editor.Servers`) and passes each service the ones it uses. When one area sets off another, the service gets the narrow piece, not the other service: `RunTests` and the workspace calls message the plan's tutor through `threads.Agents.Send`, and keep objectives written through `threads.Writers`. Tests build the services the same way, with thread agents on a fake CLI (`newTestAppWith` in `internal/app/app_test.go`). `main.go` registers them in order; Wails starts them in that order and shuts them down in reverse, and none depends on another's.

`wails3 generate bindings` reads the registered services and writes `frontend/bindings`, one folder per Go package with a module per service, one TypeScript function per exported method, and a TypeScript type for every struct those methods take or return. A service named `Service` in its package (`settings.Service`, `editor.Service`) is the module `Service` in its package's folder. `lib/api.ts` re-exports each service under its frontend name, so a component writes `TaskService.RunTests(wsID, taskID, files, false)` or `SettingsService.Settings()`. A returned Go error becomes a rejected promise.

To add a service, for a new area that fits none of the others:

1. The package of its area, or `internal/app/<area>.go` if it's about the workspaces: the service struct (`<Area>Service` in `app`, `Service` in a package of its own), its constructor taking what it uses, and its exported methods. If it owns a resource to set up or release, give it `ServiceStartup` or `ServiceShutdown` and assert them next to the type (`var _ application.ServiceShutdown = (*Service)(nil)`), so a wrong signature doesn't compile instead of never being called.
2. `internal/app/app.go`: a field on `Services`, filled in `newServices`.
3. `main.go`: `application.NewService(services.<Area>)` in the list.
4. Regenerate the bindings, export the service from `lib/api.ts` under its frontend name as the others are, and add it to the table above.

The bindings are generated, and committed. After changing an exported method or a type it returns, run `wails3 generate bindings -clean=true -ts -i` and commit the result. CI regenerates them and fails if they differ.

### Events from Go

Some things start on the Go side. The agent streams a reply, a writer publishes a task, a sandbox check finishes, a language server answers. Go emits an event for each one, and the frontend listens.

All events live in `internal/events`, apart from the services: a service, a thread agent or an MCP tool may send any of them. Each has a payload struct, a name constant in `namespace:name` form whose type names the payload (`ChatMessage Name[Chat] = "chat:message"`, named as the frontend's `AppEvents.ChatMessage`), and an `application.RegisterEvent[Payload](string(name))` call in `init()`. Registering does two things. The binding generator writes the names to `frontend/bindings/events` and types each event's data in TypeScript, and Wails drops an event whose data has the wrong type. It only logs that, and release builds discard the log, so a mistake fails silently: `TestEventsRegistered` checks that every name constant is registered with its payload type.

| Event | Payload | Sent when |
| --- | --- | --- |
| `chat:message` | `events.Chat` | a chat message is added or updated, including each streamed piece of a reply |
| `thread:busy` | `events.Busy` | a thread's tutor or writer starts or stops working |
| `check:status` | `events.Check` | a sandbox check (`events.SandboxCheck`) started or finished: a writer's new task in the quality gate, or a language a chat sets up |
| `task:published` | `events.Task` | a writer published a task |
| `lesson:published` | `events.Lesson` | a writer published a lesson |
| `questions:asked` | `events.Questions` | the tutor called `ask_user` |
| `workspace:changed` | `events.Workspace` | anything else in the workspace changed; the frontend reloads it |
| `curriculum:proposed` | `events.Workspace` | the tutor proposed a curriculum |
| `lsp:message` | `events.LSP` | a language server sent a message, or stopped |

Go code sends with `events.Emit(name, payload)`. Because the name's type carries the payload type, `events.Emit(events.ChatMessage, events.Busy{...})` doesn't compile. The compiler can't link the `RegisterEvent` calls to the names, because the binding generator needs them written out literally, so `TestEventsRegistered` checks those. `events.Emit` looks up the running Wails app with `application.Get()`. Tests run without one, and there it does nothing, so tests check what changed through the services' methods.

In the frontend, `useEvent(AppEvents.ChatMessage, (ev) => ...)` listens for as long as the component is mounted. Most payloads carry the workspace id, and views ignore events for other workspaces.

To add an event:

1. `internal/events/events.go`: the payload struct, the name constant (`Name[Payload]`) and its `application.RegisterEvent` line in `init`. `TestEventsRegistered` fails until the last two match.
2. Call `events.Emit(events.<Name>, payload)` where it happens. If the snapshot also carries the state it changes, number it from `Agents.seq` under `Agents.mu` in `internal/agent/threads/agents.go`, as `setPending` does.
3. Add it to the table above.
4. Run `wails3 generate bindings -clean=true -ts -i`, then listen in the frontend ([Frontend](#frontend), "An event").

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

A feature's state and its backend calls live in hooks next to its components. The components render what the hooks return. Pages that own their data load it themselves: a lesson (`LessonPanel`), an exercise with autosave and checks (`features/task/use-task-work.ts`), Home and Settings call their services in an effect and keep their own edits.

### The workspace screen

`features/workspace/workspace-view.tsx` lays out the curriculum rail, the centre pane and the chat panel (labelled "Tutor"). Its state comes from these hooks:

| Hook | State | Comes from |
| --- | --- | --- |
| `use-workspace.ts` | the snapshot (`WorkspaceService.Workspace`), busy threads, writers' checks and failures, a chat's toolchain check, open questions, the last error | refetched on `workspace:changed`, `task:published` and `lesson:published`; `thread:busy`, `check:status`, writers' failures from `chat:message` and `questions:asked` update it directly |
| `use-open-item.ts` | the open item, the centre's view (item, plan or proposal), the chat thread that goes with them, new work to point out | the learner, remembered per workspace; `task:published`, `lesson:published` and `curriculum:proposed` |
| `use-chat-threads.ts` | conversations by thread, and which are unread | `ChatService.Chat` for the shown thread; chats' messages from `chat:message` |
| `use-chat-panel.ts`, `chat/use-composer.ts` | the chat panel's width and whether it's folded; the message draft | the learner |

#### How data flows

All files are in `features/workspace/` unless named otherwise.

1. **Snapshot.** `use-workspace.ts: useWorkspace` loads `WorkspaceService.Workspace` and loads it again on `workspace:changed`, `task:published` and `lesson:published`.
2. **Events.** The same hook dispatches `thread:busy`, `check:status`, `questions:asked` and writers' failures from `chat:message` straight to the reducer, without a refetch.
3. **Reducer.** `workspace-state.ts: reduceWorkspace` merges both into one `LiveWorkspace`. Busy threads and writers' checks reach it both ways, in either order. Go numbers the events and the snapshots in the order they happened (`seq`, counted under `threads.Agents.mu`; see `internal/agent/threads/agents.go`), and the reducer keeps whichever is newer, so a writer that finishes during a refetch stops spinning.
4. **Selectors.** Pure functions turn that state, and what the learner has open (`use-open-item.ts: useOpenItem`), into what each part shows: `isBusy`, `tutorBusy` and `writingObjectives` in `workspace-state.ts`; `describeItem` in `open-item.ts`; `placeQuestions` and `centerOf` in `use-open-item.ts`.
5. **Components.** `workspace-view.tsx: WorkspaceView` calls them and hands their results to `Sidebar`, `CenterPane` and `ChatPanel` (or `ChatRail` when folded). They render them and call the services or the hooks' calls.

Chat transcripts are kept apart from this: `use-chat-threads.ts: useChatThreads` loads `ChatService.Chat` for the shown thread and adds each `chat:message` to it. It drops writers' messages, and `useWorkspace` keeps only writers' failures, so each message goes to one of the two.

The open item is an `OpenItem`: a task, a lesson, or an objective's lesson or exercise that isn't written yet (`open-item.ts`). `centerOf` decides what the centre shows and returns a `Center` that carries everything that page shows from the snapshot. `CenterPane` renders it, taking only busy threads, checks and the calls from `useWorkspace`. `placeQuestions` decides where the tutor's questions show: the centre, the chat of the thread that asked, and the count on "Your plan". Thread names (`plan`, a task id, `lesson-<id>`, `write-<objective>`) are the backend's and live in `lib/workspace.ts`. Only the hooks and the chat pass them around.

#### Busy

`LiveWorkspace.busy` is the only source of what the screen draws as working: the snapshot's busy threads, updated by newer `thread:busy` events. Components read it only through the selectors in `workspace-state.ts`, which `useWorkspace` returns bound to its state:

| Drawn as | Selector |
| --- | --- |
| "Thinking…", Stop in the status line, the hint button turned off | `isBusy(thread)` for the shown thread |
| the setup page's work log, the dot on "Your plan" | `isBusy(PLAN_THREAD)` |
| the spinner on the folded chat panel | `tutorBusy` |
| spinners in the curriculum, a not-written page, the empty page's title | `writingObjectives`, returned as `writing` |

#### Recipes

**A centre page**

1. Get its data. `WorkspaceSnapshot` carries only the mission of the tutor's documents, so a page for `NOTES.md` or `GLOSSARY.md` needs a field on it in Go (`WorkspaceService.Workspace`), or its own service method that the page calls itself, like `LessonPanel`. Either needs the bindings regenerated.
2. If the learner chooses the page, as they choose "Your plan": add a value to `use-open-item.ts: CenterView`, and a link in `sidebar.tsx: Sidebar` whose callback in `workspace-view.tsx: WorkspaceView` calls `nav.setView` (see `onShowPlan` and `planOpen`). The chat shows the plan thread for every view but `item`.
3. `use-open-item.ts: Center`: add a kind with everything the page shows from the snapshot.
4. `use-open-item.ts: centerOf`: decide when it shows and fill it in. Until the first plan is approved, `centerOf` returns early with `setup`, `proposal` or `plan`; add a case there if the page is needed then too.
5. `workspace-view.tsx: CenterPane`: render it. The compiler points at the missing `case`. Take busy threads and checks from `live`, the snapshot's part from the `Center`.
6. `use-open-item.test.ts`: add a `centerOf` case.

**An event** (after adding it to Go's `internal/events` and regenerating the bindings)

1. Listen with `useEvent(AppEvents.<Name>, ...)` in the hook that owns the state it changes, and ignore other workspaces' events (`workspace !== id`).
2. If it only means the snapshot changed: `use-workspace.ts: useWorkspace` calls `refresh` on it, as for `workspace:changed`. Done.
3. Otherwise, in `workspace-state.ts`: add a field to `LiveWorkspace` and `initialWorkspace`, an `Action`, and its case in `reduceWorkspace`. Dispatch the action from `useWorkspace`.
4. If the snapshot also carries that state: have Go number the event from `Agents.seq` under `Agents.mu` (`internal/agent/threads/agents.go`), as `Show` does; keep each key's number in the state (like `busySeq`); merge the snapshot's value with `newer`, as the `snapshot` case does for `busy`, so an older snapshot doesn't undo it.
5. If components need it worked out (like busy), add a selector next to `isBusy` and return it from `useWorkspace`.
6. `workspace-state.test.ts`: test the new case, and with step 4 an event arriving before an older snapshot.

**A backend call**

1. Go: an exported method on the service of its area (the table under [Calls from the frontend](#calls-from-the-frontend)). If it needs a piece the service doesn't have, add it to the service's struct and constructor, and pass it in `newServices` (`internal/app/app.go`). Run `wails3 generate bindings -clean=true -ts -i`; `<Service>.<Method>` then exists with its types.
2. `lib/api.ts`: re-export any new Go type it takes or returns, under its Go name. If the call only reads, or is safe to repeat, wrap it in `patiently` in its service's export, so it waits out a restart.
3. Call it from the hook or component that owns the state: `use-workspace.ts: useWorkspace` for the workspace's own state (like `answerQuestions`), else the page that shows the result (like `LessonPanel`).
4. On the workspace screen, pass failures to `live.fail`, which shows them above the centre pane.
5. If the call changes what the snapshot shows, have Go emit `workspace:changed`, or call `live.refresh` after it, as the hint button in `WorkspaceView` does.

### Tests

`npm test` runs [Vitest](https://vitest.dev). Tests sit next to the module they test (`*.test.ts`) and cover logic that can break: the workspace reducer, open-item rules, chat merging and when the tutor shows as thinking, and hooks that react to events. Hook tests render with `@testing-library/react` in jsdom (`// @vitest-environment jsdom`) and replace `useEvent` with a fake to send events; `use-open-item.test.ts` shows how. `features/workspace/fixtures.ts` builds workspace snapshots.

Components are styled with utility classes in their markup. The one exception is rendered markdown (lessons, task statements, tutor replies), which `prose.css` styles by element because `Markdown` renders plain HTML. Add shadcn components with `npx shadcn@latest add <name>`; they land in `components/ui/`.

## The agent and its tools

The agent is the learner's own CLI, signed in already. In a chat thread it's the tutor; in a writer's thread, it's that writer. The app runs one agent process per thread (`threads.Agents`), a conversation with its own chat. `workspace.Thread` names the four kinds, and its name is the chat's file name and what the frontend sends: `plan` for the mission and curriculum, the task id for a task, `lesson-<id>` for a lesson, and `write-<objective>` for an objective's writer.

Writers have no chat window. They write an objective's lesson and exercise in the background, and the app keeps the next two unpassed objectives written so the learner doesn't wait.

### Thread agents

`threads.Agents` (`internal/agent/threads/agents.go`) runs every thread's agent, with its own lock. Other packages use it only through a few exported methods; the rest of `threads` (the writers, the chats) uses a few more:

| Method | Used by | What it does |
| --- | --- | --- |
| `Send(w, thread, Message)` | the services | posts a message in the thread's chat, then queues it |
| `State(ws)` | `WorkspaceService.Workspace` | which threads are busy, the writers' checks, and the `seq` they were read at |
| `EndWorkspace(ws)` | `WorkspaceService.DeleteWorkspace` | ends a deleted workspace's agents and drops their MCP servers |
| `Show(ws, check)` | the MCP tools, as `mcp.Checks` | sends `check:status`, and keeps a writer's check while the writer lives |
| `queue(w, thread, msg)` | `threads.Writers` | queues a message for the thread's agent, starting the agent if it has none |
| `stop(ws, thread)`, `endAll()` | `threads.ChatService` | interrupts the thread's current turn; ends every agent as the app quits |

What it needs is passed to `threads.NewAgents` when `app.New` builds it: the `settings.Store` for the agent, model and effort, the `mcp.Server` for each thread's MCP URL, and a `threads.CLI` to open a CLI session (`threads.InstalledCLI`, over `agent.Start`; tests pass a fake from `threadstest`). `Agents.open` builds the CLI's options: the workspace's folder, the MCP URL, the thread's instructions and whether it may use subagents. It posts to the chat with `threads.Post`, like the rest of the app.

Each verb in it means one thing. A thread agent (`threadAgent`) **starts** and **ends**; its CLI session **opens** and **closes**; a message is **queued**, then **answered**; **stop** interrupts a turn. On top of it, `Send` **posts** a message in the thread's chat and queues it. When the learner sends a message, `SendMessage` adds a `[context]` block about the open task or lesson (`prompts.go`) and sends it. The thread agent's loop hands each message to the CLI, and its `transcript` (`transcript.go`) streams the reply into the chat as `chat:message` events.

- A thread's agent starts with the first message queued for its thread, in the background. Chats resume their saved conversation (`Meta.ResumeIDs`); a writer starts fresh.
- The thread is busy (`thread:busy`) from the moment a message is queued until every queued message is answered, including while the CLI opens. The thread agent's count of pending messages is the only source of this state.
- When the settings change, an idle thread agent restarts with the next message. A busy one finishes its queue first.
- A busy writer takes no second message: it's already writing its objective.
- A thread agent ends when its CLI exits, its workspace is deleted or the app quits. A writer also ends as soon as it's idle, and starting any thread but the plan ends the workspace's idle task and lesson agents.
- Busy and check events and snapshots take their numbers (`seq`) from one counter under `Agents.mu`, so the numbers order them exactly as they happened.

The calls that message an agent (`SendMessage`, `AnswerQuestions`, `RequestHint`, `WriteObjectiveNow`) return once the message is queued. They don't wait for the process to start. Their error means the message wasn't sent at all, for example because the thread name is invalid. Once a message is in the chat, anything that goes wrong with it is posted in that thread as a notice with status `failed`, and nowhere else:

- If the CLI doesn't open (it's missing or not signed in), the thread agent posts why once, however many messages were waiting. It drops them and ends, so the thread turns idle and the next message tries a fresh start.
- If a turn fails, its error is posted and the thread agent takes the next message. If the CLI exits, its error is posted, the queued messages are dropped and the next message starts a new thread agent.
- A writer has no chat, so the objective's page shows its failure.

To add a kind of thread:

1. `internal/workspace/thread.go`: a `ThreadKind`, its name in `Thread.String` and `ParseThread`, and a constructor like `WriterThread`.
2. `internal/agent/threads/prompts/<kind>.md`: its role, embedded and picked in `instructions` (`prompts.go`). The role can use `{{id}}` and `{{title}}`; `instructions` decides where the title comes from. If the learner talks in it, decide in `contextFor` what `[context]` it gets.
3. `internal/agent/threads/agents.go`: decide in `Agents.open` whether it resumes its conversation, in `queue` whether it takes a second message while busy, in `answered` whether it ends once idle, and in `start` whether starting it ends idle tutors. The lifecycle tests in `agents_test.go` show how to check each.
4. `internal/mcp/tools.go`: add the kind to the `tools` lines of the tools it gets. Its instructions list them by themselves (`{{tools}}`, from `mcp.ToolNames`).
5. `TestInstructionsFillEveryPlaceholder` in `internal/agent/threads/prompts_test.go`: add a thread of the kind.
6. `docs/GLOSSARY.md`: the kind under **Thread**; and in the frontend, its name in `lib/workspace.ts` if the UI shows it.

### MCP tools

The agent changes the app's state only through MCP tools, built on the [official Go SDK](https://github.com/modelcontextprotocol/go-sdk) (imported as `mcpsdk` in `internal/mcp`, which is named `mcp` too). `mcp.Server` serves them over HTTP on `127.0.0.1`, with a random token in each thread's URL, so a thread can only reach its own workspace. The tool handlers write to the workspace and emit events. They get the sandbox runner and the link checker from the server. `mcp` doesn't import the agents: a thread's agent passes itself to `mcp.Server.URL` as the `mcp.Checks` that show its sandbox checks, so the tools call back through that interface, and `mcp` could serve any agent.

`tools` in `internal/mcp/tools.go` lists every tool with the line its calls show as in the chat, if any, and the kinds of thread that get it:

- Every kind gets `get_state` (`state.go`).
- The chats (plan, task and lesson threads) also get `ask_user`, `set_icon` and `propose_curriculum` (`plan.go`), and `set_toolchain` (`toolchain.go`).
- Writers also get `create_task` (`task.go`) and `create_lesson` (`lesson.go`), for their own objective only.

Each tool is a `*mcpsdk.Tool` with its name and description, a handler method on `threadTools`, and its input and output types, kept together. The SDK derives the input schema from the input type's `json` and `jsonschema` tags and rejects calls that don't match it. A handler that returns an error sends the error's text back to the agent, which fixes its call and tries again. To add a tool:

1. The file of its area in `internal/mcp` (`plan.go`, `task.go`, ...; a new one for a new area): the `*mcpsdk.Tool` (its description is the agent's instructions for it), the input and output types, and the handler method on `threadTools`.
2. `internal/mcp/tools.go`: a line in `tools` with the line the learner sees in the chat for each call ("Saved your notes"), or `hidden` if the calls are noise or show in their own UI, then the kinds that get it. A tool for the plan only is a line with `workspace.PlanKind`. Each thread's instructions list its tools from here, and the chat shows its calls by it (`mcp.ChatLine`).
3. `internal/agent/threads/prompts/shared.md`, under "Tools from the `tutortop` MCP server": a line saying when to use it; the role prompts (`plan.md`, `writer.md`, ...) if one of them should call it at a set point.
4. A test in `internal/mcp/tools_test.go` through `mcptest.Caller`, for what the handler checks beyond the schema.
5. If a handler changes what the workspace screen shows, emit `workspace:changed` or its own event.

`create_task` publishes a task only after it passes the quality gate in the sandbox. The reference solution has to pass three times in shuffled order, the starter code has to fail, and the tests have to catch at least two wrong solutions.

## Checks and the editor

**Run** and **Submit** call `TaskService.RunTests`. It saves the learner's code, runs the visible tests (plus the hidden ones on submit) in the sandbox and records the attempt. The sandbox images are built ahead, in the background, so the first test run is fast: when the app starts (`WorkspaceService.ServiceStartup`, for every workspace's plan language) and when the learner approves a plan. Reading a workspace never starts a build, since the frontend repeats reads freely.

The editor's completions and errors come from a language server running in the task's sandbox image. Go doesn't read the LSP messages. It relays them between the server's stdio and the frontend, sending them out as `lsp:message` events and taking them in through `EditorService.SendLanguageServer` (`internal/editor`). The framing and the check that a new toolchain's server answers (`sandbox.ProbeLanguageServer`) are in `internal/sandbox/lsp.go`, as `set_toolchain` uses the check too.

### Languages

A toolchain says how one language's tests run in the sandbox and how their results are read (`sandbox.Toolchain`). The built-in ones are in `internal/sandbox/toolchain.go`. Any other language is set up by the tutor with `set_toolchain` and saved in the workspace as `.tutortop/toolchains/<language>.yaml` (`sandbox.ToolchainSpec`, `internal/workspace/toolchains.go`); its tests must write a JUnit report.

To add a built-in language:

1. `internal/sandbox/toolchain.go`: a `Toolchain` and its entry in `builtins`. Its Dockerfile installs the language, the test framework and a language server, and runs as user 1000 (`runner`) in `/work` with no network, like the others. Tag the image `tutortop-<language>:1`, and bump the tag whenever the Dockerfile changes. `script` runs the tests `repeat` times, shuffled if the framework can.
2. The same `Toolchain`'s `parse`, which reads the results, and `readable`, which turns the output into text for the learner. A framework that writes JUnit can use `junitScript`, `parseJUnitRuns` and `stripJUnit`, as Python does. One with its own format needs its own, tested on recorded output in `internal/sandbox` (see `TestRunGo` and `TestParseJUnit`).
3. `internal/agent/threads/prompts/shared.md`, under "Language conventions": a line `- **<Name> <version>**: …` saying how its tasks are laid out. The prompts' lists of built-in languages (`{{builtins}}`) and `set_toolchain`'s description fill themselves in from `sandbox.BuiltinLanguages`.
4. The `jsonschema` struct tags in `internal/mcp`, `internal/workspace` and `internal/sandbox` that list the other built-ins (such as `stateOutput.Toolchains` in `internal/mcp/state.go`): tags can't be filled in, so add the name by hand.
5. Search the prompts for its name: `set_toolchain`'s line in `shared.md` gives R, Rust and JavaScript as examples of languages the app doesn't ship.
6. `internal/workspace/icons.go`: a picture in `Icons`, if there's none for it.

A workspace whose tutor set the language up before it was built in keeps its own toolchain: `Workspace.Toolchain` looks in the workspace first, because that workspace's tasks passed the quality gate with it. `set_toolchain` refuses to set up a built-in language anew.
