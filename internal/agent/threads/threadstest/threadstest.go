// Package threadstest fakes the agent CLI thread agents open their sessions
// with (threads.CLI), so tests never start a real agent.
package threadstest

import (
	"context"
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
		return &Session{Prompts: new([]string), Mu: &sync.Mutex{}}, nil
	}
}

// Session is an agent session that takes every message, adding it to
// Prompts, and never answers.
type Session struct {
	Prompts *[]string
	Mu      *sync.Mutex // guards Prompts when the test reads them concurrently
}

func (s *Session) Prompt(_ context.Context, text string) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	*s.Prompts = append(*s.Prompts, text)
	return nil
}

func (s *Session) ID() string   { return "" }
func (s *Session) Cancel()      {}
func (s *Session) Close() error { return nil }
