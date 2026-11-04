// Package threadstest fakes the agent CLI thread agents open their sessions
// with (threads.CLI), so tests never start a real agent.
package threadstest

import (
	"context"
	"slices"
	"sync"

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
		return &Session{}, nil
	}
}

// Session is an agent session that takes every message and never answers.
type Session struct {
	mu      sync.Mutex
	prompts []string
}

func (s *Session) Prompt(_ context.Context, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prompts = append(s.prompts, text)
	return nil
}

// Prompts returns the messages the session has taken so far.
func (s *Session) Prompts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.prompts)
}

func (s *Session) ID() string   { return "" }
func (s *Session) Cancel()      {}
func (s *Session) Close() error { return nil }
