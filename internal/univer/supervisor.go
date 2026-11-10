package univer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
)

// SupervisorOptions describes the collaboration server process this binary
// starts for single-host deployments. Container deployments run the server as
// its own service and leave Managed off.
type SupervisorOptions struct {
	Entry       string
	Port        int
	Secret      string
	DatabaseURL string
}

// Supervisor owns the child process lifetime.
type Supervisor struct {
	opts SupervisorOptions

	mu     sync.Mutex
	cmd    *exec.Cmd
	cancel context.CancelFunc
	done   chan struct{}
}

// NewSupervisor builds a supervisor for the given entry script.
func NewSupervisor(opts SupervisorOptions) *Supervisor {
	return &Supervisor{opts: opts}
}

// Start launches `node <entry>`. A missing node binary or entry script is
// reported so startup logs make the misconfiguration obvious.
func (s *Supervisor) Start(parent context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd != nil {
		return nil
	}
	if _, err := os.Stat(s.opts.Entry); err != nil {
		return fmt.Errorf("collaboration server entry %q: %w", s.opts.Entry, err)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		return fmt.Errorf("node is required to run the collaboration server: %w", err)
	}
	ctx, cancel := context.WithCancel(parent)
	cmd := exec.CommandContext(ctx, node, s.opts.Entry)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("UNIVER_PORT=%d", s.opts.Port),
		fmt.Sprintf("UNIVER_SHARED_SECRET=%s", s.opts.Secret),
	)
	if s.opts.DatabaseURL != "" {
		cmd.Env = append(cmd.Env, "UNIVER_DB=postgres", "DATABASE_URL="+s.opts.DatabaseURL)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		cancel()
		return err
	}
	s.cmd = cmd
	s.cancel = cancel
	s.done = make(chan struct{})
	go func() {
		defer close(s.done)
		_ = cmd.Wait()
	}()
	// Give the server a moment to bind; the first request retries anyway.
	select {
	case <-s.done:
		cancel()
		s.cmd = nil
		return fmt.Errorf("collaboration server exited during startup")
	case <-time.After(300 * time.Millisecond):
	}
	return nil
}

// Stop terminates the child process.
func (s *Supervisor) Stop() {
	s.mu.Lock()
	cmd, cancel, done := s.cmd, s.cancel, s.done
	s.cmd, s.cancel, s.done = nil, nil, nil
	s.mu.Unlock()
	if cmd == nil {
		return
	}
	if cancel != nil {
		cancel()
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
}
