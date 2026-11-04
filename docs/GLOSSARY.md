# Glossary

The words the code uses, what each means, and where it lives. Go identifiers are in `internal/`, TypeScript ones in `frontend/src/`. (Not to be confused with the `GLOSSARY.md` the tutor writes in a learner's workspace.)

## Workspaces and conversations

- **Workspace**: one topic the learner studies, kept as plain files and a git repo in `~/Tutortop/<id>`, plus private files outside it. Go: `workspace.Workspace`, opened through `workspace.Store`.
- **Snapshot**: everything the workspace screen shows, read at one moment. Go: `app.WorkspaceSnapshot`, returned by `App.Workspace`; its `Seq` orders it among the `thread:busy` and `check:status` events. The frontend keeps it current between refetches as `LiveWorkspace` (`workspace-state.ts`), the snapshot plus what events changed since; `useWorkspace` returns that plus its calls (`UseWorkspaceResult`).
- **Agent**: the learner's own coding-agent CLI (Claude Code, Codex), which Tutortop drives. Go: `agent.Provider` for each kind; the workspace remembers which one it last used in `Meta.Provider`. Every thread, tutor or writer, runs on the agent, model and effort chosen in Settings (`Settings.Provider` and `Settings.Models`, together an `agentConfig`; `Settings.Provider` is the `agent` key in `settings.yaml`), and its instructions start with the rules in `prompts/shared.md`.
- **Thread**: one conversation with the agent in a workspace, named by a string that is also its chat's file name. Go: `workspace.Thread` with a `ThreadKind`; TS: `PLAN_THREAD`, `lessonThread`, `writerThread` (`lib/workspace.ts`). The kinds:
  - **plan** (`PlanKind`, `"plan"`): the interview, the mission and the curriculum.
  - **task** (`TaskKind`, the task id): help with one task.
  - **lesson** (`LessonKind`, `"lesson-<id>"`): questions about one lesson.
  - **writer** (`WriterKind`, `"write-<objective>"`): writes one objective's lesson and task in the background. It has no chat window.
- **Chat**: a thread's transcript as the learner sees it, a list of chat items saved in `.tutortop/chat/<thread>.jsonl`. Plan, task and lesson threads are the **chats**. Go: `workspace.ChatItem`, with a `ChatRole`, a `ChatStatus` for tool calls and failures, and a `ChatMark` for items code looks for (`MarkPlanApproved`).
- **Chat panel**: the column beside the centre pane that shows the chat of whatever is open. The UI labels it "Tutor". TS: `ChatPanel`, folded into `ChatRail` (`features/chat/`).
- **Thread agent**: one thread's running agent in the app: its session and the queue of messages it answers one turn at a time. A thread agent starts and ends; its session opens and closes; a message is queued, then answered; stop interrupts a turn. Go: `app.threadAgent`, run by `threadAgents` (`agents.go`, `App.agents`).
- **Session**: one conversation with the agent's CLI, in one running process. Go: `agent.Session`. Its id (`Session.ID`) is saved per chat as a **resume id** in `Meta.ResumeIDs` (the `sessions` key of `.tutortop/workspace.json`), so the chat resumes its conversation.
- **Subagent**: a helper the agent starts for part of its work, such as research. The plan and writers may use them; task and lesson chats don't, as a learner waits for the reply. A subagent finishes within the turn and follows the same tool policy. Go: `agent.Options.Subagents`, set in `App.agentOptions`; a call that starts one is a `Tool` with `Subagent` set, shown in the chat as "Researching: …".
- **Turn**: one message to the agent and everything it does until it answers. Go: one `agent.Session.Prompt` call. A thread is **busy** while it has turns queued or running (`thread:busy`).
- **Tutor**: the agent answering in a chat (plan, task or lesson thread), as opposed to a writer. Its role is `prompts/plan.md`, `task.md` or `lesson.md`, after the shared rules. TS: `tutorBusy` means some chat's tutor is working.
- **Writer**: the agent in a writer thread. It publishes its objective's lesson and task with `create_lesson` and `create_task`, then ends. Go: `writer.go`, `prompts/writer.md`; the app keeps the next `readyAhead` objectives written, and `App.WriteObjectiveNow` writes one the learner opens ahead.

## The plan

- **Mission**: why the learner is learning this and what they want to build, in `MISSION.md`. The tutor writes it; the learner can edit it.
- **Curriculum**: the approved plan, in `curriculum.yaml`: the default language and modules of objectives. Go: `workspace.Curriculum`.
- **Proposal**: a curriculum the tutor suggests with `propose_curriculum`, saved in `.tutortop/proposal.yaml` until the learner approves or rejects it. Go: `Workspace.Propose`, `ApproveProposal`; `WorkspaceSnapshot.Proposal`.
- **Revision**: the number of an approved curriculum, counting up from 1 with each approval. Go: `Curriculum.Revision`.
- **Module**: a chapter of the curriculum, a group of objectives with the references it's based on. Go: `workspace.Module`.
- **Objective**: one thing the learner should be able to do, provable by unit tests. Each has one lesson and its tasks. Go: `workspace.Objective`, ids like `go.errors.wrap`.

## Learning and practice

- **Lesson**: the theory for one objective, read before its task, ending in a quiz. Go: `workspace.LessonInfo` (`lessons/<id>/lesson.yaml`), `LessonDetail` for the screen, `LessonSpec` from `create_lesson`.
- **Quiz**: a lesson's multiple-choice questions, checked by the app at once. Results help the tutor plan but never count as progress. Go: `workspace.QuizResult`.
- **Task**: an exercise for one objective, also called an exercise in the UI: a statement, files to edit and tests. Go: `workspace.TaskInfo` (`tasks/<id>/task.yaml`), `TaskDetail` for the screen, `TaskSpec` from `create_task`.
- **Starter**: the files the learner starts from. They must compile and fail the tests. Go: `TaskSpec.Starter`; the learner's copy is in `tasks/<id>/work/`.
- **Reference**: a correct solution the writer submits with the task. It's kept private and never shown. Go: `TaskSpec.Reference`.
- **Mutant**: a plausible wrong solution the tests must catch. Go: `runner.Mutant`.
- **Hidden tests**: edge-case tests that run only on submit. The learner sees their names and failure messages, not their code. Go: `TaskSpec.HiddenTests`, kept in the private folder.
- **Attempt**: one learner action recorded in `.tutortop/attempts.jsonl`: a run, a submit, a hint or a quiz answer. Go: `workspace.Attempt` with an `AttemptKind` (`AttemptRun`, `AttemptSubmit`, `AttemptHint`, `AttemptQuiz`).
- **Progress**: which objectives are passed, derived from tasks and attempts only. A task passes with a submit that passes every test. Go: `workspace.Progress`. Not to be confused with **activity**, the learner's days and streak across workspaces on Home (`App.Activity`, `workspace.Activity`).

## The sandbox

- **Sandbox**: the Docker or Podman container tests run in, built from the toolchain's image. Go: `runner.Runner`; `Runner.Available` says whether Docker or Podman works, and `Runner.Prepare` builds a toolchain's image ahead of the first run (`App.prepareImage`). "Prepare" means only that.
- **Test run**: running a task's tests on the learner's code: the visible ones for **Run**, the hidden ones too for **Submit**. Go: `App.RunTests`, returning a `runner.Result`, recorded as an attempt.
- **Quality gate**: the check a new task must pass before the learner sees it: the reference passes three times in shuffled order, the starter fails, and at least two mutants are caught. Go: `runner.Gate`, reporting a `runner.GateReport`.
- **Sandbox check**: a check an agent waits for and the learner watches: a writer's task in the quality gate, or a language a chat sets up. In the code, "check" means only this. Go: `app.SandboxCheck` with a `CheckKind`, sent as `check:status`; TS: `LiveWorkspace.checks` (by objective) and `toolchainCheck`.
- **Toolchain**: how one language's tests run and are read. **Built-in** ones ship with the app (`runner.Builtin`, `BuiltinLanguages`); **custom** ones are set up by the tutor with `set_toolchain` and saved per workspace (`runner.ToolchainSpec`, `.tutortop/toolchains/<language>.yaml`). A workspace's own toolchain wins over a built-in of the same language.
- **Language server**: the program in the sandbox image that gives the editor completions and errors. Go: `Toolchain.LanguageServer`, relayed by `lsp.go` as `lsp:message`.

## Between the parts

- **MCP tool**: a function the agent calls to change the app's state, served by Tutortop's MCP server. Go: `mcpTools` lists each with the line its calls show as in the chat (or `hidden`) and the thread kinds that get it, and each thread's instructions list its tools from it (`{{tools}}`); `threadTools` handles one thread's calls.
- **Event**: a message Go pushes to the frontend when something happens it didn't ask about. Go: `event[Data]` constants in `events.go`, sent with `emit`; TS: `AppEvents`, heard with `useEvent`.
