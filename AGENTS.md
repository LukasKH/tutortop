### Tools
- Use rg over grep

### Tests
- Test behaviour that can actually break; skip anything the types already guarantee.
- Tests on main are smoke tests: a few whole flows driven through the `App` methods the frontend calls, with a fake agent and the real sandbox, plus the agent permission rules. A new feature extends one of these flows instead of adding unit tests for its functions.
- Unit tests written while building a feature are fine on the branch. Before merging, delete those that don't guard a flow.
- The Go↔frontend contract is the generated `frontend/bindings`. After changing an exported `App` method or a type it returns, run `wails3 generate bindings -clean=true -ts -i` and commit the result; CI regenerates them and fails if they differ.
- Tests that call the real Claude Code or Codex CLIs go behind `//go:build live` and run by hand with `task test:live`.
