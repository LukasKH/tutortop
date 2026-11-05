# Style guide

How we write code in Tutortop. Reviews check changes against this guide. Rules marked **(lint)** are also checked by the pre-commit hooks.

## Defaults first

Follow each library's documented structure and idioms: Wails v3 services and bindings, React and the Vite template, shadcn/ui, the Go standard library. If you need to deviate, explain why in a comment.

Prefer the standard library or a maintained library over hand-rolled code.

## File layout

Put what a file offers first: its exported types and functions, the main one at the top. Put helpers below the code that uses them.

In Go, keep a type's constructor and methods next to the type. In TypeScript, keep a module-level value above its first use at load time.

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

Handle the error or the special case first and return, so the main path stays at the left margin.

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

Split a function that branches a lot into one function per part, each named for what it does **(lint: gocognit, nestif)**. A validator for nested data, for example, gets one `validate` method per type, each called by its parent's.

## TypeScript and React

Follow the rules of hooks **(lint)**.

Give a component only what it renders. Don't pass props down more than two levels; let the hook that owns the state provide it.

Re-export generated binding types under their Go names. Don't edit the generated bindings; regenerate them.

## Comments

Keep comments short. Explain why, not what.

## Tests

Test behaviour that can break: translating a CLI's protocol, permission and path guards, files on disk and their old formats, parsing outside formats, races, and the logic a bug fix changed. Each test is a contract: break the behaviour and the test fails.

Leave out what the compiler or CI already checks (an interface assertion, a registration the generated bindings cover), trivial logic, wording, smoke runs, asserts that a fake was called the way the code calls it, and one validator case per `if`.

Test through a module's interface, the way its callers use it.

Test at one layer: the lowest one that would fail. Don't repeat a case in a wrapper's test.

Extend before you add. When an existing test or table covers the behaviour, add a row or an assertion there.
