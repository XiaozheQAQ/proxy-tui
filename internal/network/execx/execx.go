// Package execx runs external commands behind an interface so platform
// adapters can be tested without touching the host configuration.
package execx

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// Runner executes a command and returns its combined output.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (stdout string, stderr string, err error)
}

// OSRunner executes real commands.
type OSRunner struct{}

// Run implements Runner.
func (OSRunner) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// Call records one invocation.
type Call struct {
	Name string
	Args []string
}

// Result is a canned command result.
type Result struct {
	Stdout string
	Stderr string
	Err    error
}

// Fake is a scripted Runner for tests. Respond decides the result per call.
type Fake struct {
	mu      sync.Mutex
	calls   []Call
	respond func(name string, args []string) Result
}

// NewFake builds a fake runner. respond may be nil, in which case every
// command succeeds with empty output.
func NewFake(respond func(name string, args []string) Result) *Fake {
	return &Fake{respond: respond}
}

// Run implements Runner.
func (f *Fake) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, Call{Name: name, Args: append([]string(nil), args...)})
	respond := f.respond
	f.mu.Unlock()
	if respond == nil {
		return "", "", nil
	}
	result := respond(name, args)
	return result.Stdout, result.Stderr, result.Err
}

// Calls returns a copy of every recorded invocation.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Call, len(f.calls))
	copy(out, f.calls)
	return out
}

// Commands renders the recorded invocations for assertions.
func (f *Fake) Commands() []string {
	calls := f.Calls()
	out := make([]string, 0, len(calls))
	for _, call := range calls {
		out = append(out, strings.TrimSpace(call.Name+" "+strings.Join(call.Args, " ")))
	}
	return out
}

// Error builds a command failure with a readable message.
func Error(name string, args []string, stderr string) error {
	return fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), strings.TrimSpace(stderr))
}
