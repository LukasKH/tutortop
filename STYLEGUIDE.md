# Style guide

How code in Tutortop is written. Reviews check changes against it. Rules marked **(lint)** also fail the pre-commit hooks.

## Defaults first

- Follow each library's documented structure and idioms before inventing our own: Wails v3 services and bindings, React and the Vite template, shadcn/ui, the Go standard library. If you must deviate, say why in a comment.
- Prefer the standard library or a maintained library over hand-rolled logic.

## File layout

- Start each file with what it offers: its exported types and functions, the main one first. Helpers and internal details go below the code that uses them, close to their caller ("newspaper" or step-down style).
- In Go, keep a type's constructor and methods together with the type.
- In TypeScript, a module-level value that's used while the module loads must stay above its first use.

## Go

### Services

- The frontend talks to Wails services, one per area: workspaces, tasks, chat, settings, and so on. A service is a struct whose exported methods are the frontend's API, registered in `main.go`. (Wails v3: "one service, one purpose".)
- A service gets its dependencies through its constructor. No package-level mutable state.

### Dependencies

- A dependency is a named type with methods, like `*workspace.Store`, or a small interface defined in the package that uses it. Not a function field.

  ```go
  // Don't
  type App struct {
  	findBroken func(ctx context.Context, urls []string) []string
  }

  // Do
  type App struct {
  	links *linkChecker // linkChecker{client *http.Client}
  }
  ```

- Function fields are for hooks that customise how a type behaves, like `exec.Cmd.Cancel`, not for passing in dependencies.
- Add an interface only at a boundary that tests really replace: the agent's CLI, HTTP, the sandbox. A type with one implementation and no fake needs no interface.
- Use the standard test seams: `httptest` for HTTP, `t.TempDir()` for files, the fake CLI in `internal/agent` for agent CLIs.

### State

- Data lives with its owner: a workspace's data in `internal/workspace`, the app's settings in the settings store. A service doesn't keep its own map of another package's data.
- Prefer a method on the owner (`w.LastRun(taskID)`) over a map keyed by a composite key (`map[taskKey]runner.Result`).

### Constructors and literals

- Keep struct literals plain. A value that needs explaining gets a named helper with its own comment, not a comment inside the literal.

  ```go
  // Don't
  return &threadAgents{openSession: openSession, config: config, options: options, agents: map[threadKey]*threadAgent{},
  	// Counting from the time keeps the numbers rising across a restart.
  	seq: uint64(time.Now().UnixMicro())}

  // Do
  return &threadAgents{settings: settings, mcp: mcp, cli: cli, agents: map[threadKey]*threadAgent{}, seq: firstSeq()}
  ```

### Return values, not out-parameters

- A function that produces a value returns it, typed, so the call reads as one line: the result and its error on the left, the type on the right. Use a generic function or method (Go 1.27 allows type parameters on methods) instead of declaring a variable and passing a pointer to fill.

  ```go
  // Don't
  var meta Meta
  err := json.Unmarshal(b, &meta)

  // Do
  meta, err := decode.JSON[Meta](b)
  ```

- Existing helpers: `decode.JSON[T]`, `decode.YAML[T]` and `decode.XML[T]` (internal/decode), `readJSON[T]` and `readYAML[T]` for files (internal/workspace), `rpcConn.Call[T]`. Use them, or add one like them, rather than unmarshalling into a pointer. The helper itself is the one place that calls the standard library with `&v`.
- Pass a pointer instead when:
  - **decoding into a value that already holds data**, such as merging a file over defaults. The standard library's decoders take a pointer for this reason, even the new `encoding/json/v2`.
  - **decoding many items in a loop.** Reuse one value instead of creating a new one each time, so it isn't allocated on every pass. `Unmarshal` leaves fields that are missing from the input untouched, so reset the value first:

    ```go
    var ev goTestEvent
    for line := range strings.Lines(out) {
    	ev = goTestEvent{} // or the previous line's fields leak into this one
    	if json.Unmarshal([]byte(line), &ev) != nil {
    		continue
    	}
    	…
    }
    ```

  - **an interface needs the method.** Interface methods can't have type parameters (Go spec), so a seam's method takes or returns a concrete type, and the generic call stays in a helper.

### Types and control flow

- An enum-like value gets a named type and constants. A switch over one covers every case **(lint: exhaustive)**.
- Keep functions shallow: return early, and split a function that nests deeply or branches a lot **(lint: gocognit, nestif)**.

## TypeScript and React

- Follow the rules of hooks **(lint)**.
- A component gets what it renders. Don't pass the same data twice, and don't drill props through more than two levels; let the hook that owns the state hand it out.
- Re-export generated binding types under their Go names. Never edit `frontend/bindings`; regenerate it.

## Comments

- Short and plain. Say why, not what the code already says.

## Tests

- Test behaviour that can actually break; skip anything the types already guarantee.
- Test through a module's interface, the way its callers use it.
