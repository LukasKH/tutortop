// Package threadstest fakes the agent CLI thread agents open their sessions
// with (threads.CLI), so tests never start a real agent.
package threadstest

import (
	"context"

	"github.com/LukasKH/tutortop/internal/agent"
)

// CLI is a threads.CLI that opens sessions by calling itself.
type CLI func(ctx context.Context, provider string, opts agent.Options, onEvent func(agent.Event)) (agent.Session, error)

func (f CLI) Start(ctx context.Context, provider string, opts agent.Options, onEvent func(agent.Event)) (agent.Session, error) {
	return f(ctx, provider, opts, onEvent)
}

// Quiet is a CLI whose sessions take every message and never answer.
func Quiet() CLI {
	return func(context.Context, string, agent.Options, func(agent.Event)) (agent.Session, error) {
		return quietSession{}, nil
	}
}

type quietSession struct{}

func (quietSession) Prompt(context.Context, string) error { return nil }
func (quietSession) ID() string                           { return "" }
func (quietSession) Cancel()                              {}
func (quietSession) Close() error                         { return nil }
