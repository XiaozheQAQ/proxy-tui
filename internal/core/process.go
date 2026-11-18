package core

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Process owns one proxy core child process and streams its output to a log
// sink.
type Process struct {
	mu         sync.Mutex
	cmd        *exec.Cmd
	done       chan error
	startedAt  time.Time
	stopped    bool
	lastErr    string
	configPath string
	apiAddr    string
	logf       func(level, msg string)
}

// NewProcess creates an idle process handle.
func NewProcess(logf func(level, msg string)) *Process {
	if logf == nil {
		logf = func(string, string) {}
	}
	return &Process{logf: logf}
}

// Running reports whether the child is still alive.
func (p *Process) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cmd != nil && !p.stopped
}

// PID returns the child pid, or 0.
func (p *Process) PID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

// StartedAt is the launch time.
func (p *Process) StartedAt() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.startedAt
}

// LastError returns the exit error of the last run.
func (p *Process) LastError() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastErr
}

// ConfigPath returns the working directory used for the last launch.
func (p *Process) ConfigPath() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.configPath
}

// APIAddr returns the control API address used for the last launch.
func (p *Process) APIAddr() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.apiAddr
}

// Start launches binary with args and streams stdout/stderr into the sink.
func (p *Process) Start(binary string, args []string, env []string, workDir, apiAddr string) error {
	p.mu.Lock()
	if p.cmd != nil && !p.stopped {
		p.mu.Unlock()
		return ErrAlreadyRunning
	}
	cmd := exec.Command(binary, args...)
	cmd.Env = append(os.Environ(), env...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		p.mu.Unlock()
		return fmt.Errorf("capture stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		p.mu.Unlock()
		return fmt.Errorf("capture stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		p.mu.Unlock()
		return fmt.Errorf("start core: %w", err)
	}
	done := make(chan error, 1)
	p.cmd = cmd
	p.done = done
	p.startedAt = time.Now()
	p.stopped = false
	p.lastErr = ""
	p.configPath = workDir
	p.apiAddr = apiAddr
	p.mu.Unlock()

	go p.stream(stdout)
	go p.stream(stderr)

	go func() {
		waitErr := cmd.Wait()
		p.mu.Lock()
		p.stopped = true
		if waitErr != nil {
			p.lastErr = waitErr.Error()
		}
		p.mu.Unlock()
		if waitErr != nil {
			p.logf("warn", "core exited: "+waitErr.Error())
		} else {
			p.logf("info", "core exited cleanly")
		}
		done <- waitErr
		close(done)
	}()
	return nil
}

func (p *Process) stream(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		p.logf("info", scanner.Text())
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, os.ErrClosed) {
		p.logf("debug", "core output closed: "+err.Error())
	}
}

// Stop terminates the child, escalating to kill after timeout.
func (p *Process) Stop(timeout time.Duration) error {
	p.mu.Lock()
	cmd := p.cmd
	done := p.done
	if cmd == nil || cmd.Process == nil || p.stopped {
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()

	if err := terminate(cmd.Process); err != nil {
		p.logf("warn", "graceful stop failed: "+err.Error())
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		if err := cmd.Process.Kill(); err != nil {
			return fmt.Errorf("kill core: %w", err)
		}
		<-done
		return nil
	}
}
