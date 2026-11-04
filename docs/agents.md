# Adding an agent

`internal/agent` drives the learner's own coding-agent CLI: Claude Code over its stream-json stdio protocol, Codex over `codex app-server` JSON-RPC. Each CLI has an adapter. The adapter turns what the CLI prints into a few events and holds the tutor to Tutortop's tool policy. To add an agent (Gemini CLI, OpenCode, anything that speaks the Agent Client Protocol), the work is in this package, plus the few edits [outside it](#steps).

The contract is in code. `agent.go` has the types and their doc comments, and `policy.go` has the tool policy. This page explains them and adds the steps.

## Steps

An adapter is one file, like `claude.go` or `codex.go`, and an entry in a list:

1. `internal/agent/<x>.go`: `startX(ctx, opts, onEvent) (Session, error)`, which starts the CLI and returns a `Session` that keeps [the contract](#the-contract).
2. The same file: `xModels(ctx) ([]Model, error)`, which lists the models it offers, the CLI's default first, each with the reasoning efforts it takes. Claude's list is fixed, because the CLI takes aliases that follow each release. Codex's is asked live from the CLI. The app caches the answer for a few minutes.
3. `internal/agent/agent.go`: an entry in `Providers()` with an id, a display name, the binary to look up on `PATH`, and the two functions.
4. `internal/agent/<x>_test.go`: tests against a fake CLI, including the shared policy cases (`testPolicy`); see [Testing an adapter](#testing-an-adapter).
5. `internal/agent/live_test.go`: add the id to the provider list in `TestLive`. `internal/app/live_test.go`'s `TestLiveInterview` runs a whole interview on any agent: `TUTORTOP_PROVIDER=<id> task test:live`.
6. `Taskfile.yml`: the `test:live` summary, and `.github/workflows/ci.yml`: the comment above the `test` job. Both name the CLIs the live tests talk to.
7. `go.mod` and `go.sum`, for an SDK the adapter uses, such as `github.com/coder/acp-go-sdk`.
8. `internal/shellpath/shellpath.go`: `commonDirs`. An app started from a desktop launcher doesn't get the user's shell PATH, so it asks the login shell and falls back to these folders, where the agents' installers put their binaries. Add the new CLI's folder if it isn't there; OpenCode's install script, for one, uses `~/.opencode/bin`.
9. `README.md`: the introduction and **Install** name the agents Tutortop runs on, with a link to each one's setup.

Nothing else changes. Settings list `Providers()` and show the ones that aren't installed as missing. The frontend gets each provider's display name from `App.Environment`, so a new agent needs no frontend change. The prompts in `internal/app/prompts` don't name an agent, and the chat shows Tutortop's own tools by their `mcpTools` line, whatever title the CLI gives the call.

A workspace remembers one agent (`Meta.Provider`) and, for each chat, the `Session.ID()` to resume (`Meta.ResumeIDs`). Those ids belong to one agent, so when a thread starts with another agent, `Workspace.UseProvider` switches the workspace to it and forgets them: every thread starts a fresh conversation, and the tutor catches up from the workspace files.

## The contract

### Starting

`Start` returns once the CLI is ready for a prompt and the session has an id. Do any handshake (initialize, sign-in check, new or resumed session) before returning, under its own timeout, so a CLI that hangs can't block the app. The process runs in `opts.Cwd`, the learner's workspace.

When start fails, return an error the learner can act on. If the CLI isn't installed, `startProcess` already says so. If it isn't signed in, say which command to run. If it exited, return `process.abort(err)`, which wraps `ErrExited` around the end of the CLI's stderr.

### Options

Every field must be honoured:

- `MCPName` and `MCPURL`: Tutortop's MCP server, a streamable HTTP endpoint. It must be the only MCP server the agent sees, under exactly this name, because the policy allows that server's tools and no others. Allow its tool calls at least 10 minutes, because `create_task` runs the sandbox checks.
- `Instructions`: add these to the agent's system prompt. Keep the CLI's own prompt.
- `ResumeID`: an id from an earlier `Session.ID()`. If the CLI can't resume it (the transcript was deleted, or it came from another machine), start a fresh session and return its id. Don't fail.
- `Model` and `Effort`: ids from your `Models` list. An empty string means the CLI's default.

### A turn

`Prompt` sends the learner's message and blocks until the turn ends. The app sends one prompt at a time. It returns:

| Outcome | Return | The session afterwards |
| --- | --- | --- |
| the turn ended | `nil` | takes the next prompt |
| `Cancel` ended it | `nil` | takes the next prompt |
| the agent reports the turn failed (usage limit, API error) | an error with the agent's reason | takes the next prompt |
| the process exited | an error wrapping `ErrExited` | finished; the app starts a new one |
| `ctx` ended first | `ctx.Err()` | the turn may still be running |

The second row is easy to get wrong. CLIs often report an interrupted turn as a failure, and Claude Code does. The adapter has to remember that it asked for the interrupt.

`Cancel` asks the CLI to end the current turn. It must not block, may be called from any goroutine, and does nothing without a turn. `Close` stops the process without waiting (callers may hold a lock) and may be called twice. `startProcess` sends an interrupt first, so the CLI can save its session.

### Events

`onEvent` gets events from one goroutine, in the order the CLI reported them. Deliver every event of a turn before `Prompt` returns. In practice that means calling `onEvent` from the same read loop that sees the end of the turn, before it wakes `Prompt`.

- `EventText` is the next piece of the reply to the learner. Joined, the pieces are the reply. Don't send reasoning or thinking text. The app starts a new message after each tool call.
- `EventTool` is a tool call starting or changing status. Its first event describes the call in full: `Kind`, `Title`, `Paths`, and `MCPServer`/`MCPTool` for MCP tools. Later events for the same `ID` may carry only `ID` and `Status`. End every call with `ToolDone` or `ToolFailed`; a declined call is `ToolFailed`. Ids must be unique within the session.
- `EventWarning` is something the learner must know. Send one if the CLI reports that Tutortop's MCP server didn't connect, since without it the tutor can talk but can't create tasks or ask questions. Send it once, not on every turn.

`ToolKind` matters beyond display. The app hides reads, searches and `think`. The policy decides by kind, so a call that changes files must be `ToolEdit` and carry its paths.

### The tool policy

The tutor may read and change files in the workspace, except under `tasks/`, which the app manages through `create_task`. It may use Tutortop's MCP tools and the web. It may not run commands, use other MCP servers, or start work that outlives the turn, such as background subagents.

Enforce this in two layers:

1. **Configure the CLI** so it refuses what it can by itself: no shell tool, no writes under `tasks/` or outside the workspace, only our MCP server. Never use a mode that approves everything. Claude uses `--disallowedTools` with an `Edit(./tasks/**)` deny rule, `--strict-mcp-config` and `acceptEdits`. Codex uses a permission profile, with the workspace writable and `tasks/` read-only, plus `approvalPolicy: "untrusted"`.
2. **Answer every permission request** the CLI still makes with `policy.allow(tool)`. Describe the call as a `Tool`, with its kind and the paths it touches. If a request names a file change by id only, remember the paths from when the item started, as Codex's adapter does. Refuse anything you can't describe, or don't recognise: unknown request types get an error or a decline, never an approval. The reason `allow` returns goes back to the agent, so pass it on if the protocol has room for it.

Build the policy with `newPolicy(opts)` when the session starts.

## Shared pieces

| File | What it gives an adapter |
| --- | --- |
| `process.go` | `startProcess` finds and starts the CLI with piped stdio; `send` writes one line of JSON; `await` waits for a reply unless the process exits first; `abort` and `exitError` turn an exit into `ErrExited` with stderr. `decode[T]` decodes a message or a field of one. |
| `rpc.go` | `rpcConn`, newline-delimited JSON-RPC: `Call[T]`, `Notify`, and handlers for the CLI's notifications and requests. |
| `policy.go` | `policy.allow`, the one place that decides tool calls. |
| `agent.go` | `optional`, for titles like `"Searched files" + optional(": ", pattern)`. |

`rpcConn` is hand-rolled because Codex's app-server leaves out the `"jsonrpc":"2.0"` field, and most Go JSON-RPC 2.0 libraries reject messages without it. A library would replace about 100 lines, and it would have to tolerate Codex's dialect. Use `rpcConn` for a JSON-RPC CLI. For an ACP agent, use the ACP SDK's connection instead (see below).

## Testing an adapter

`fakecli_test.go` lets a test play the CLI. `fakeBinary(t, "mycli")` puts the test binary on `PATH` as `mycli`. When the adapter starts it, `TestMain` turns it into a relay between its stdio and the test. The adapter runs unchanged, through the real `startProcess`, and the test:

- reads what the adapter wrote with `cli.recv(t)`, which returns a `message` with a dotted-path `get`/`str`;
- writes the CLI's output with `cli.send(t, lines...)`. Strings go as they are, so recorded transcripts can be pasted in;
- makes the CLI exit with `cli.exit(t, code, stderr)`;
- checks how the CLI was started through `cli.Args`, `cli.Env` and `cli.Dir`.

`Start` and `Prompt` block, so run them with `async` and collect their result with `wait`. `eventLog` records events. Every wait times out, so a test that leaves the adapter hanging fails rather than stalling the run. `claude_test.go` and `codex_test.go` show the pattern. Start with their `startFakeClaude` and `startFakeCodex` helpers, which play the handshake.

Cover what can break:

- the start arguments and settings that carry the policy, the MCP server, the instructions, the model and the effort;
- resume, including an id the CLI no longer has;
- a turn's text deltas, tool calls starting, finishing and failing, and the turn ending;
- a failed turn as an error, after which the next prompt still works;
- `Cancel` sending the CLI's interrupt, and the interrupted turn returning `nil`;
- the process exiting during start and during a turn, giving `ErrExited` with stderr;
- the MCP warning, if the CLI reports MCP status;
- the model list;
- the policy: call `testPolicy(t, cwd, ask)` from `policy_test.go`. `ask` makes your fake CLI request permission for a `Tool` and reports whether the adapter allowed it. It runs the shared cases: shell refused, edits under `tasks/` refused, paths outside the workspace refused, our MCP server allowed and others refused. A case your CLI never asks about calls `t.Skip` with the reason.

Get the real messages from the CLI's own schema or by recording a session. `codex app-server generate-json-schema --out DIR` writes Codex's schema.

The live tests (`//go:build live`, step 5) check the real CLI end to end, and `task test:live` runs them.

## An ACP agent

The Agent Client Protocol is JSON-RPC over stdio. It maps onto this package closely, which is why `ToolKind` uses ACP's kinds. [`github.com/coder/acp-go-sdk`](https://github.com/coder/acp-go-sdk) is a maintained Go SDK generated from the ACP schema. Gemini CLI and OpenCode speak ACP natively. Zed's `claude-agent-acp` and `codex-acp` adapters need Node, and the app must not depend on Node.

| This package | ACP |
| --- | --- |
| `startX` | start the binary with `startProcess`, then `acp.NewClientSideConnection(client, proc.stdin, proc.stdout)`, `Initialize` (don't offer the terminal capability) and `NewSession` with `Cwd` and an HTTP `McpServers` entry for `MCPName`/`MCPURL` |
| `ResumeID` | `LoadSession`, if the agent offers it; otherwise start fresh |
| `Prompt` | `Prompt`; the `StopReason` is `end_turn` or `cancelled` → `nil`, `refusal`, `max_tokens` and the like → an error |
| `Cancel` | the `session/cancel` notification |
| `EventText` | `session/update` with `agent_message_chunk` (skip `agent_thought_chunk`) |
| `EventTool` | `tool_call` (full: kind, title, locations → `Paths`, and `MCPServer`/`MCPTool` as below) and `tool_call_update`; status `pending`/`in_progress` → running, `completed` → done, `failed` → failed |
| `policy.allow` | `session/request_permission`: describe its `toolCall` the same way, then pick an allow-once or reject-once option |
| `xModels` | the session's config options (`configOptions` in the `NewSession` reply): the select with category `model` gives the models, the one with category `thought_level` the efforts. ACP reports them per session only, so start the binary in a temporary folder, `Initialize`, `NewSession`, read the options and `Close`, as `codexModels` does with Codex |
| `Model`, `Effort` | after `NewSession` or `LoadSession`, `session/set_config_option` with the option's `id` and the chosen value; leave an empty one alone |
| `Instructions` | the agent's own way to add to its system prompt: a flag, an environment variable or a config file it reads at start (see OpenCode below). ACP's stable schema has no system-prompt field; a proposed `systemPrompt` on `session/new` (mode `append`) fits exactly, so use it if the agent advertises it |

An ACP tool call doesn't say which MCP server its tool belongs to, so the adapter has to work out `MCPServer` and `MCPTool` itself. Both matter: `policy.allow` lets an MCP call through only when `MCPServer` is `opts.MCPName`, and a call without it is judged by its kind, which for an MCP tool is usually `other` and refused. Left empty, every Tutortop tool call would be refused, and the tutor couldn't ask a question or publish a task. The chat also needs them, to show our tools by their `mcpTools` line or hide them. Take them from what the agent reports, in its own spelling of MCP tool names: the tool call's `name` if the agent sends one, else its `title`, `rawInput` or `_meta` (Claude Code, for one, names our tools `mcp__tutortop__get_state`). Record a session to see the real form, and split off exactly `opts.MCPName` and the agent's separator, so a server called `tutortop2` isn't taken for ours. `testPolicy`'s "use our MCP server" case checks it, once your fake CLI asks for our tool in the agent's spelling.

An agent that has no model or thought-level option returns a single `Model` with an empty `ID` and no efforts, which tells the app to leave the choice to the CLI. Don't fall back to sending the instructions as the first prompt: a resumed session would get them twice, and the agent may drop them when it compacts its context.

Don't offer the terminal capability. If you offer file access, put each read and write through the policy too. Pass the SDK's connection the `process` pipes, so `ErrExited`, stderr and `Close` work the same as for the other adapters.

The SDK's `acp.Client` interface, which `NewClientSideConnection` takes, has every method an agent may call on the client, whatever the client offers: `SessionUpdate`, `RequestPermission`, `ReadTextFile`, `WriteTextFile`, and the terminal methods `CreateTerminal`, `TerminalOutput`, `WaitForTerminalExit`, `KillTerminal` and `ReleaseTerminal`. An agent shouldn't call the file or terminal ones without the capability, but the adapter must still have them: make each one you don't offer return an error, so a call is refused, never carried out. When `Cancel` ends a turn, ACP requires the client to answer each pending `session/request_permission` with the `cancelled` outcome.

### OpenCode's instructions and policy

OpenCode reads its settings from `opencode.json`, and the `OPENCODE_CONFIG` environment variable points it at another file. Write one for the session outside the workspace, so the tutor can't change it, and set it in the process's environment:

- `instructions` lists files OpenCode adds to its prompt: write `opts.Instructions` to a file next to the config and name it there.
- `permission` is the first layer of the policy: `"bash": "deny"`, `"task": "deny"` (no subagents), `"external_directory": "deny"`, and `edit` as `{"*": "allow", "tasks/**": "deny"}` (the last matching rule wins). Rules match MCP tools by name too.

OpenCode merges this file with the user's own config, so their MCP servers can still load. `policy.allow` refuses those servers' tools, like anything else the config lets through to a permission request.
