# Style guide

How we write code in Tutortop. Reviews check changes against this guide. Rules marked **(lint)** are also checked by the pre-commit hooks.

## Defaults first

Follow each library's documented structure and idioms. If you need to deviate, explain why in a comment.

Prefer the standard library or a maintained library over hand-rolled code.

## Function Grouping and Ordering

- Functions should be sorted in rough call order.
- Functions in a file should be grouped by receiver.

Therefore, exported functions should appear first in a file, after
`struct`, `const`, `var` definitions.

A `newXYZ()`/`NewXYZ()` may appear after the type is defined, but before the
rest of the methods on the receiver.

Since functions are grouped by receiver, plain utility functions should appear
towards the end of the file.

```go
// Good:
type something struct{ ... }

func newSomething() *something {
	return &something{}
}

func (s *something) Cost() int {
	return calcCost(s.weights)
}

func (s *something) Stop() {...}

func calcCost(n []int) int {...}

// Bad:
func (s *something) Cost() int {
	return calcCost(s.weights)
}

type something struct{ ... }

func calcCost(n []int) int {...}

func (s *something) Stop() {...}

func newSomething() *something {
	return &something{}
}
```

## Go

### Packages

Group code by what a package provides, and name the package after it, such as `billing` or `email`. Don't group by layer (`services`, `models`, `handlers`). Don't make grab-bag packages (`util`, `common`, `shared`); give shared code a package named for what it does.

Don't repeat the package name in its identifiers.

```go
// Good:
cfg, err := config.Load(path)

// Bad:
cfg, err := config.LoadConfig(path)
```

Keep tightly coupled types in one package. If a caller must import both packages to use either, merge them.

### Services

Give each area of the frontend's API its own Wails service, and register each in `main.go`.

Pass a service its dependencies through its constructor, not through package-level variables.

### Dependencies

Pass dependencies as named types, not as functions, in both fields and parameters. A reader then sees what something uses from its type name, without working out a function signature.

```go
// Good:
type Checkout struct {
	prices *PriceList
}

func NewCheckout(prices *PriceList, payments *Payments) *Checkout

// Bad:
type Checkout struct {
	priceOf func(ctx context.Context, sku string) (int, error)
}

func NewCheckout(priceOf func(ctx context.Context, sku string) (int, error), charge func(Order) error) *Checkout
```

A function is fine when the caller hands it in to be called back: a handler (`onEvent func(Event)`), a cleanup (`stop func()`), or the change to apply (`update(change func(*Order))`).

Use a concrete type. Define an interface only when a test needs a fake or to break an import cycle, and define it in the package that uses it.

When a library looks for an interface at runtime, such as Wails' `ServiceShutdown`, assert it so a wrong signature fails to compile:

```go
var _ application.ServiceShutdown = (*OrderService)(nil)
```

### State

Keep data in the package that owns it, behind its methods.

```go
// Good:
order, ok := store.Order(id)

// Bad:
order, ok := app.orders[orderKey{customerID, id}]
```

### Arguments

Don't pass a bare `true` or `false` when the call doesn't show what it means. Give each case its own function or method, or give the parameter a named type with constants. A `/* name */` comment next to the value doesn't count: nothing checks it.

```go
// Good:
func (r *Report) pass(name, detail string) {
	r.Steps = append(r.Steps, Step{Name: name, OK: true, Detail: detail})
}

func (r *Report) fail(name, detail string) {
	r.Steps = append(r.Steps, Step{Name: name, OK: false, Detail: detail})
}

rep.fail("has tests", "Add at least one test file.")
rep.pass("reference passes", "")

// Bad:
add := func(name string, ok bool, detail string) { rep.Steps = append(rep.Steps, Step{name, ok, detail}) }

add("has tests", false, "Add at least one test file.")
add("reference passes", true, "")
```

A bool that already has a name at the call site is fine: `run(job, submit)`, `engineHint(out, errors.Is(err, exec.ErrNotFound))`.

### Return values

Return values instead of filling a pointer. A generic function puts the result and its type on one line.

```go
// Good:
cfg, err := parse[Config](b)

// Bad:
var cfg Config
err := json.Unmarshal(b, &cfg)
```

Use a pointer when performance calls for it, such as reusing one value while decoding many items in a loop. Reset it on each pass: `Unmarshal` keeps fields the input doesn't set.

A nil slice is a valid empty slice. Start from `var s []T` and append, return `nil` for no results, and check `len(s) == 0`, not `s == nil`.

```go
// Good:
func activeNames(users []User) []string {
	var names []string
	for _, u := range users {
		if u.Active {
			names = append(names, u.Name)
		}
	}
	return names
}

if len(names) == 0 {
	return errNoActiveUsers
}

// Bad:
func activeNames(users []User) []string {
	names := []string{}
	for _, u := range users {
		if u.Active {
			names = append(names, u.Name)
		}
	}
	return names
}

if names == nil {
	return errNoActiveUsers
}
```

Exception: return `[]T{}` when the frontend receives the slice as JSON. A nil slice encodes as `null`, and the frontend expects `[]`.

```go
// Good:
func (s *OrderService) Orders() ([]Order, error) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Order{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Order{}
	for _, e := range entries {
		out = append(out, orderFrom(e))
	}
	return out, nil
}

// Bad: no orders reach the frontend as null.
func (s *OrderService) Orders() ([]Order, error) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Order
	for _, e := range entries {
		out = append(out, orderFrom(e))
	}
	return out, nil
}
```

### Errors

Handle every error a call returns **(lint: errcheck)**. When nothing can be done with one, assign it to `_` and say why in a comment. Return the error from `Close` on a file you wrote to: a failed write can show up only there.

```go
// Good:
_, err = f.Write(b)
return errors.Join(err, f.Close())

defer func() { _ = f.Close() }() // read only: nothing to lose

// Bad:
defer f.Close()
_, err = f.Write(b)
return err
```

### Struct literals

Write a multi-line literal one field per line, each ending with a comma, and put the closing brace on its own line. A short comment after a field is fine.

```go
// Good:
return &Cache{
	items: map[string]Item{},
	ttl:   time.Minute,
	seq:   uint64(time.Now().UnixMicro()), // rises across restarts
}

// Bad:
return &Cache{items: map[string]Item{}, ttl: time.Minute,
	seq: uint64(time.Now().UnixMicro())}
```

Name the fields in a struct literal. Leave the names out only for a type with two or three fields whose order is obvious, such as a map key or a table row. Leave out fields set to their zero value unless the zero means something, such as an index of 0 or a test case's expected result.

```go
// Good:
key := threadKey{wsID, name}
streak := Streak{Today: 0, Current: 2, Best: 5}

// Bad:
streak := Streak{0, 2, 5}
user := User{Name: "Ada", Admin: false, Nickname: ""}
```

Declare an empty struct with `var`, and a pointer to a new struct with `&T{...}`, not `new(T)` **(lint: pre-commit no-new-struct)**. `var x T` and `x := T{}` compile to the same thing; `var` tells the reader the value starts empty and gets filled in later.

```go
// Good:
var rep Report
cache := &Cache{ttl: time.Minute}

// Bad:
rep := Report{}
cache := new(Cache)
cache.ttl = time.Minute
```

### Embedding in structs

Put embedded fields at the top of a struct, then a blank line before the regular fields **(lint: embeddedstructfieldcheck)**. An embedded type adds its fields and methods to the outer type, so a reader should see it first.

Embed a type only when the outer type should offer every exported method and field of the inner one. Otherwise, use a named field and write the methods the outer type needs. Don't embed a mutex: callers could lock your type from outside **(lint: embeddedstructfieldcheck)**. Embedding a pointer makes the promoted methods panic on a zero value, so embed one only when a constructor always sets it.

```go
// Good:
type TaskStatus struct {
	TaskInfo // its fields sit at the top level of the JSON

	Passed bool `json:"passed"`
}

type Cache struct {
	mu    sync.Mutex
	items map[string]Item
}

// Bad:
type TaskStatus struct {
	Passed bool `json:"passed"`
	TaskInfo
}

type Cache struct {
	sync.Mutex // callers can call cache.Lock()
	items map[string]Item
}
```

### Control flow

Give enum-like values a named type and constants, and cover every case when you switch on one **(lint: exhaustive)**. A new value then fails the lint at every switch that doesn't handle it yet, instead of silently doing nothing. A `default:` case doesn't count as covering them.

```go
// Good:
type Status int

const (
	Pending Status = iota
	Paid
	Shipped
)

switch o.Status {
case Pending:
	remind(o)
case Paid:
	ship(o)
case Shipped:
	track(o)
}

// Bad:
if o.Status == "pending" {
	remind(o)
} else if o.Status == "paid" {
	ship(o)
}
```

Handle the error or the special case first and return, so the main path stays at the left margin **(lint: revive)**. The lint catches an `if`/`else` where one branch ends in `return`, `continue` or `break`; review catches the rest.

```go
// Good:
if o.Coupon == "" {
	return nil
}
if err := coupons.Check(o.Coupon); err != nil {
	return fmt.Errorf("coupon %q: %w", o.Coupon, err)
}
return nil

// Bad:
if o.Coupon != "" {
	if err := coupons.Check(o.Coupon); err != nil {
		return fmt.Errorf("coupon %q: %w", o.Coupon, err)
	}
}
return nil
```
## Unnecessary Else
When both branches of an `if` set the same variable, set the default first and drop the `else`. Keep the `else` when the default calls a function, since hoisting it would run the call every time.

```go
// Good:
discount := 0
if o.Coupon != "" {
	discount = 10
}

// Bad:
var discount int
if o.Coupon != "" {
	discount = 10
} else {
	discount = 0
}
```

Split a function that branches a lot into one function per part, each named for what it does **(lint: gocognit, nestif)**. A validator for nested data, for example, gets one `validate` method per type, each called by its parent's.

## TypeScript and React

Follow the rules of hooks **(lint)**.

Give a component only what it renders. Don't pass props down more than two levels; let the hook that owns the state provide it.

Re-export generated binding types under their Go names. Don't edit the generated bindings; regenerate them.

## Comments

Keep comments short. Explain why, not what.

## Tests

Test behaviour that can break. Each test is a contract: break the behaviour and the test fails.

Leave out what the compiler or CI already checks (an interface assertion, a registration the generated bindings cover).

Test through a module's interface, the way its callers use it.

Test at one layer: the lowest one that would fail. Don't repeat a case in a wrapper's test.

Extend before you add. When an existing test or table covers the behaviour, add a row or an assertion there.
