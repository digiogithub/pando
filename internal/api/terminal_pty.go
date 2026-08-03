package api

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/google/uuid"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
)

const (
	// ptyReplayBytes is how much recent output is kept so a reconnecting client
	// (page reload, dropped socket) can repaint its screen.
	ptyReplayBytes = 256 * 1024
	// ptyReadChunk is the size of a single read from the PTY master.
	ptyReadChunk = 32 * 1024
	// ptySessionIdleTTL is how long a shell with no attached client survives.
	ptySessionIdleTTL = 30 * time.Minute
	// ptySubscriberBuffer is the per-client backlog before the PTY pump blocks.
	// Blocking is intentional: dropping bytes would corrupt the screen state,
	// and a real terminal applies back-pressure to the shell too.
	ptySubscriberBuffer = 64
)

// ptySession is a shell running in a real PTY, fed to zero or more websocket
// clients. Unlike the one-shot /terminal/exec path, the shell is long-lived and
// owns its own screen state; the server only pumps bytes.
type ptySession struct {
	id     string
	cmd    *exec.Cmd
	master *os.File
	shell  string
	dir    string

	mu       sync.Mutex
	replay   []byte
	subs     map[chan []byte]struct{}
	lastUsed time.Time
	exited   bool
	exitCode int

	done chan struct{}
}

// ptyShellCommand picks the shell for the PTY terminal. Interactive mode is
// correct here (unlike the PTY-less /terminal/exec path): the shell has a
// controlling terminal, so job control works and ~/.zshrc aliases load.
func ptyShellCommand() (string, []string) {
	var shell string
	var args []string
	if cfg := config.Get(); cfg != nil {
		shell = strings.TrimSpace(cfg.Shell.Path)
		args = append([]string(nil), cfg.Shell.Args...)
	}
	if shell == "" {
		shell = choosePreferredShell()
		args = nil
	}
	if len(args) == 0 {
		args = []string{"-i"}
	}
	return shell, args
}

// maxPTYDim bounds the terminal dimensions accepted from API clients. It keeps
// the pixel sizes (cols*8, rows*16) inside the uint16 range of pty.Winsize, so
// an oversized request cannot wrap around into a tiny or bogus window size.
const maxPTYDim = 2000

// clampPTYDim normalizes a client-supplied terminal dimension to a sane range,
// falling back to def when the value is unusable.
func clampPTYDim(v, def int) int {
	if v < 2 {
		return def
	}
	if v > maxPTYDim {
		return maxPTYDim
	}
	return v
}

func newPTYSession(cwd string, cols, rows int) (*ptySession, error) {
	cols = clampPTYDim(cols, 80)
	rows = clampPTYDim(rows, 24)

	shell, args := ptyShellCommand()
	cmd := exec.Command(shell, args...)
	cmd.Dir = cwd
	cmd.Env = buildShellEnv(os.Environ())

	master, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
		X:    uint16(cols * 8),
		Y:    uint16(rows * 16),
	})
	if err != nil {
		return nil, fmt.Errorf("start shell in pty: %w", err)
	}

	session := &ptySession{
		id:       uuid.NewString(),
		cmd:      cmd,
		master:   master,
		shell:    shell,
		dir:      cwd,
		subs:     map[chan []byte]struct{}{},
		lastUsed: time.Now(),
		done:     make(chan struct{}),
	}

	logging.Info("terminal.pty: shell started", "id", session.id, "shell", shell, "args", args, "pid", cmd.Process.Pid)
	go session.pump()
	return session, nil
}

// pump is the single reader of the PTY master. It appends to the replay buffer
// and fans out to every attached client.
func (s *ptySession) pump() {
	defer s.finish()

	buf := make([]byte, ptyReadChunk)
	for {
		n, err := s.master.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			s.broadcast(chunk)
		}
		if err != nil {
			// Read returns EIO on Linux when the child closes the slave side;
			// that is a normal shell exit, not a failure worth logging loudly.
			return
		}
	}
}

func (s *ptySession) broadcast(chunk []byte) {
	s.mu.Lock()
	s.replay = appendReplay(s.replay, chunk)
	subs := make([]chan []byte, 0, len(s.subs))
	for sub := range s.subs {
		subs = append(subs, sub)
	}
	s.mu.Unlock()

	for _, sub := range subs {
		select {
		case sub <- chunk:
		case <-s.done:
			return
		}
	}
}

// appendReplay keeps the tail of the output stream, bounded to ptyReplayBytes.
func appendReplay(replay, chunk []byte) []byte {
	replay = append(replay, chunk...)
	if len(replay) > ptyReplayBytes {
		replay = replay[len(replay)-ptyReplayBytes:]
	}
	return replay
}

func (s *ptySession) finish() {
	exitCode := 0
	if err := s.cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	s.mu.Lock()
	s.exited = true
	s.exitCode = exitCode
	subs := make([]chan []byte, 0, len(s.subs))
	for sub := range s.subs {
		subs = append(subs, sub)
	}
	s.subs = map[chan []byte]struct{}{}
	s.mu.Unlock()

	close(s.done)
	for _, sub := range subs {
		close(sub)
	}
	_ = s.master.Close()
	logging.Info("terminal.pty: shell exited", "id", s.id, "exit_code", exitCode)
}

// subscribe returns the replay buffer plus a channel of subsequent output. The
// replay and the subscription are taken under the same lock so no byte is lost
// or duplicated between them.
func (s *ptySession) subscribe() ([]byte, chan []byte, func()) {
	sub := make(chan []byte, ptySubscriberBuffer)

	s.mu.Lock()
	if s.exited {
		replay := append([]byte(nil), s.replay...)
		s.mu.Unlock()
		close(sub)
		return replay, sub, func() {}
	}
	replay := append([]byte(nil), s.replay...)
	s.subs[sub] = struct{}{}
	s.lastUsed = time.Now()
	s.mu.Unlock()

	unsubscribe := func() {
		s.mu.Lock()
		if _, ok := s.subs[sub]; ok {
			delete(s.subs, sub)
			s.lastUsed = time.Now()
		}
		s.mu.Unlock()
		// Drain so a blocked broadcast can finish instead of leaking the pump.
		go func() {
			for range sub {
			}
		}()
	}
	return replay, sub, unsubscribe
}

func (s *ptySession) write(data []byte) error {
	_, err := s.master.Write(data)
	return err
}

func (s *ptySession) resize(cols, rows int) error {
	if cols < 2 || rows < 2 {
		return nil
	}
	cols = clampPTYDim(cols, 80)
	rows = clampPTYDim(rows, 24)
	return pty.Setsize(s.master, &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
		X:    uint16(cols * 8),
		Y:    uint16(rows * 16),
	})
}

func (s *ptySession) close() {
	select {
	case <-s.done:
		return
	default:
	}
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	_ = s.master.Close()
}

func (s *ptySession) hasClients() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.subs) > 0
}

func (s *ptySession) idleSince() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastUsed
}

// ptyRegistry keeps PTY sessions alive across websocket reconnects so a page
// reload reattaches to the same shell instead of starting a new one.
type ptyRegistry struct {
	mu       sync.Mutex
	sessions map[string]*ptySession
}

var ptySessions = &ptyRegistry{sessions: map[string]*ptySession{}}

func (r *ptyRegistry) get(id string) (*ptySession, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	session, ok := r.sessions[id]
	if !ok {
		return nil, false
	}
	select {
	case <-session.done:
		delete(r.sessions, id)
		return nil, false
	default:
	}
	return session, true
}

func (r *ptyRegistry) add(session *ptySession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reapLocked()
	r.sessions[session.id] = session
}

func (r *ptyRegistry) remove(id string) {
	r.mu.Lock()
	session, ok := r.sessions[id]
	delete(r.sessions, id)
	r.mu.Unlock()
	if ok {
		session.close()
	}
}

// reapLocked kills shells that exited or have been idle with no client attached.
func (r *ptyRegistry) reapLocked() {
	cutoff := time.Now().Add(-ptySessionIdleTTL)
	for id, session := range r.sessions {
		select {
		case <-session.done:
			delete(r.sessions, id)
			continue
		default:
		}
		if !session.hasClients() && session.idleSince().Before(cutoff) {
			session.close()
			delete(r.sessions, id)
		}
	}
}
