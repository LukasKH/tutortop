package events

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"strings"
	"testing"
)

// TestEventsRegistered checks that every event is registered with Wails for
// the data its name's type says it carries. The compiler checks each emit
// against the name's type, but not the registrations, which the binding
// generator needs as literal RegisterEvent calls. Wails drops an event whose
// data doesn't match its registration.
func TestEventsRegistered(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "events.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string]string{}   // event name constant -> data type
	registered := map[string]string{} // same, from the RegisterEvent calls
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.ValueSpec: // ChatMessage Name[Chat] = "chat:message"
			if typ, ok := n.Type.(*ast.IndexExpr); ok && types.ExprString(typ.X) == "Name" {
				for _, name := range n.Names {
					declared[name.Name] = types.ExprString(typ.Index)
				}
			}
		case *ast.CallExpr: // application.RegisterEvent[Chat](string(ChatMessage))
			if fn, ok := n.Fun.(*ast.IndexExpr); ok && types.ExprString(fn.X) == "application.RegisterEvent" {
				name := strings.TrimSuffix(strings.TrimPrefix(types.ExprString(n.Args[0]), "string("), ")")
				registered[name] = types.ExprString(fn.Index)
			}
		}
		return true
	})
	if len(declared) == 0 {
		t.Fatal("found no events in events.go")
	}
	if !maps.Equal(declared, registered) {
		t.Fatalf("event names declare data types %v,\nbut they're registered as %v", declared, registered)
	}
}
