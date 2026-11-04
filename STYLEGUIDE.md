# Style guide

How we write code in Tutortop. Reviews check changes against this guide. Rules marked **(lint)** are also checked by the pre-commit hooks.

## Defaults first

Follow each library's documented structure and idioms: Wails v3 services and bindings, React and the Vite template, shadcn/ui, the Go standard library. If you need to deviate, explain why in a comment.

Prefer the standard library or a maintained library over hand-rolled code.

## File layout

Put what a file offers first: its exported types and functions, the main one at the top. Put helpers below the code that uses them.

In Go, keep a type's constructor and methods next to the type. In TypeScript, keep a module-level value above its first use at load time.

## Go

### Services

Give each area of the frontend's API its own Wails service: workspaces, tasks, chat, settings, and so on. Register them in `main.go`.

Pass a service its dependencies through its constructor, not through package-level variables.

### Dependencies

Make dependencies named types, not function fields.

```go
// Good:
type mcpHost struct {
	links *linkChecker
}

// Bad:
type mcpHost struct {
	findBroken func(ctx context.Context, urls []string) []string
}
```

Use a concrete type. Define an interface only when a test needs a fake, and define it in the package that uses it.

When a library looks for an interface at runtime, such as Wails' `ServiceShutdown`, assert it so a wrong signature fails to compile:

```go
var _ application.ServiceShutdown = (*ChatService)(nil)
```

### State

Keep data in the package that owns it.

```go
// Good:
run, ok := w.LastRun(taskID)

// Bad:
run, ok := a.lastResults[taskKey{wsID, taskID}]
```

### Return values

Return values instead of filling a pointer. Use a generic helper, such as `decode.JSON[T]`, so the result and its type read on one line.

```go
// Good:
meta, err := decode.JSON[Meta](b)

// Bad:
var meta Meta
err := json.Unmarshal(b, &meta)
```

Use a pointer when performance calls for it, such as reusing one value while decoding many items in a loop. Reset it on each pass: `Unmarshal` keeps fields the input doesn't set.

### Struct literals

Keep struct literals plain. Move a value that needs explaining into a named function with its own comment.

```go
// Good:
return &threadAgents{settings: settings, agents: map[threadKey]*threadAgent{}, seq: firstSeq()}

// Bad:
return &threadAgents{settings: settings, agents: map[threadKey]*threadAgent{},
	// Counting from the time keeps the numbers rising across a restart.
	seq: uint64(time.Now().UnixMicro())}
```

### Control flow

Give enum-like values a named type and constants, and cover every case when you switch on one **(lint: exhaustive)**.

Return early instead of nesting. Split a function that branches a lot **(lint: gocognit, nestif)**.

## TypeScript and React

Follow the rules of hooks **(lint)**.

Give a component only what it renders. Don't pass props down more than two levels; let the hook that owns the state provide it.

Re-export generated binding types under their Go names. Don't edit `frontend/bindings`; regenerate it.

## Comments

Keep comments short. Explain why, not what.

## Tests

Test behaviour that can break. Skip what the types already guarantee.

Test through a module's interface, the way its callers use it.
