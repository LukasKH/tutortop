## Best practices
- Ensure pre commits do not fail
- Always aim to follow established defaults and best practices in with in the technologies of this project. Avoid hand-rolled complex logic unless necessary.
- Start each file with what it offers: its exported types and functions, the main one first. Helpers and internal details go below the code that uses them, close to their caller. In Go, keep a type's constructor and methods together with the type. In TypeScript, a module-level value that's used while the module loads must stay above its first use. "newspaper" or step-down style

### Tools
- Use rg over grep


### Tests
- Test behaviour that can actually break; skip anything the types already guarantee.
