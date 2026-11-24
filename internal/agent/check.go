package agent

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/LukasKH/tutortop/internal/decode"
	"github.com/LukasKH/tutortop/internal/subproc"
	"github.com/LukasKH/tutortop/internal/text"
)

// Check is what trying an agent's CLI found: its version, the account it's
// signed in with, and the models it offers. Problem says what went wrong and
// how to fix it; "" means the agent is ready.
type Check struct {
	Version string `json:"version,omitempty"`
	// Account is the plan signed in with, such as "Claude Max", "ChatGPT
	// Plus" or "Google AI Pro"; "" if the CLI doesn't say.
	Account string  `json:"account,omitempty"`
	Limits  []Limit `json:"limits,omitempty"` // for CLIs that report them
	Models  []Model `json:"models,omitempty"`
	Problem string  `json:"problem,omitempty"`
}

// Limit is how much of one of the account's usage limits is used.
type Limit struct {
	UsedPercent   float64 `json:"usedPercent"`
	WindowMinutes int     `json:"windowMinutes"`      // how long the limit runs, e.g. 300 for five hours
	ResetsAt      string  `json:"resetsAt,omitempty"` // RFC 3339
}

// Check tries the provider's CLI, binary as in Options.Binary: it asks for
// the version, the account and the models. Codex and Antigravity answer from
// a short session, which also proves they're signed in.
func (p Provider) Check(ctx context.Context, binary string) Check {
	binary = cmp.Or(binary, p.Binary)
	var c Check
	version, err := output(ctx, binary, "--version")
	if err != nil {
		c.Problem = fmt.Sprintf("%s didn't run: %v", binary, err)
		return c
	}
	c.Version = versionPattern.FindString(version)
	switch p.ID {
	case Claude:
		if c.Account, err = claudeAccount(ctx, binary); err == nil {
			c.Models, err = claudeModels(ctx)
		}
	case Codex:
		var info codexInfo
		info, err = codexCheck(ctx, binary)
		c.Account, c.Limits, c.Models = info.account, info.limits, info.models
	case Antigravity:
		c.Models, c.Account, err = askAntigravity(ctx, binary)
	}
	if err != nil {
		c.Problem = err.Error()
	}
	return c
}

// versionPattern finds the version in a CLI's --version line, such as
// "2.1.331 (Claude Code)" or "codex-cli 0.172.0".
var versionPattern = regexp.MustCompile(`\d+\.\d+(\.\d+)?[\w.+-]*`)

// claudeAccount reads `claude auth status`, which prints JSON.
func claudeAccount(ctx context.Context, binary string) (string, error) {
	out, err := output(ctx, binary, "auth", "status")
	status, jsonErr := decode.JSON[struct {
		LoggedIn         bool   `json:"loggedIn"`
		AuthMethod       string `json:"authMethod"`
		SubscriptionType string `json:"subscriptionType"`
	}]([]byte(out))
	if jsonErr != nil {
		return "", fmt.Errorf("couldn't read Claude Code's sign-in: %w", cmp.Or(err, jsonErr))
	}
	if !status.LoggedIn {
		return "", fmt.Errorf("not signed in to Claude Code: run `%s` in a terminal and sign in, then check again", binary)
	}
	if status.SubscriptionType != "" {
		return "Claude " + upperFirst(status.SubscriptionType), nil
	}
	return status.AuthMethod, nil
}

// output runs a CLI briefly and returns what it printed to stdout, kept
// apart from stderr so a warning there can't spoil JSON. An error carries the
// start of stderr (or of stdout if stderr is empty), which usually says
// what's wrong.
func output(ctx context.Context, binary string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, binary, args...)
	subproc.HideWindow(cmd)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err != nil {
		if msg := cmp.Or(strings.TrimSpace(stderr.String()), strings.TrimSpace(stdout.String())); msg != "" {
			err = fmt.Errorf("%w: %s", err, text.Truncate(msg, 200))
		}
	}
	return stdout.String(), err
}

func upperFirst(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}
