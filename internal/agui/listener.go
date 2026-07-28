package agui

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/digiogithub/pando/internal/logging"
)

// ListenerOptions describes the dedicated listener requested by Config.Port.
type ListenerOptions struct {
	// Host to bind to. Empty means localhost, never all interfaces: this
	// surface drives an agent that executes code, so exposing it on 0.0.0.0 has
	// to be a deliberate act.
	Host string
	// CertFile and KeyFile enable TLS. Both or neither.
	CertFile string
	KeyFile  string
}

// Listener is a running dedicated AG-UI listener.
type Listener struct {
	server *http.Server
	addr   string
	tls    bool
	errCh  chan error
}

// Addr is the address the listener actually bound to.
func (l *Listener) Addr() string { return l.addr }

// URL is the base URL clients should point CopilotKit at.
func (l *Listener) URL() string {
	scheme := "http"
	if l.tls {
		scheme = "https"
	}
	return scheme + "://" + l.addr
}

// Wait blocks until the listener stops, returning the reason. A clean shutdown
// reports nil.
func (l *Listener) Wait() error {
	err := <-l.errCh
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown stops the listener. It does not close the Runtime: the caller owns
// that, because the same Runtime may also be mounted elsewhere.
func (l *Listener) Shutdown(ctx context.Context) error {
	return l.server.Shutdown(ctx)
}

// StartListener serves the adapter on its own port, carrying only the AG-UI
// routes.
//
// This is deployment shape 2 of invariant I7: a browser origin that can reach
// the AG-UI endpoint then cannot reach the Web-UI API at all — no sessions
// list, no config, no file endpoints, no static UI. It is the recommended shape
// for anything beyond localhost.
//
// The listener is bound synchronously so a port clash is reported to the caller
// instead of being lost in a goroutine.
func (r *Runtime) StartListener(opts ListenerOptions) (*Listener, error) {
	if r.cfg.Port <= 0 {
		return nil, errors.New("agui: no dedicated port configured")
	}
	host := opts.Host
	if host == "" {
		host = "localhost"
	}
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", r.cfg.Port))

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("agui: listen on %s: %w", addr, err)
	}

	useTLS := opts.CertFile != "" && opts.KeyFile != ""
	srv := &http.Server{
		Handler:     r.Handler(),
		ReadTimeout: 30 * time.Second,
		// No write timeout: responses are long-lived SSE streams.
		WriteTimeout: 0,
	}

	l := &Listener{server: srv, addr: ln.Addr().String(), tls: useTLS, errCh: make(chan error, 1)}
	go func() {
		if useTLS {
			l.errCh <- srv.ServeTLS(ln, opts.CertFile, opts.KeyFile)
			return
		}
		l.errCh <- srv.Serve(ln)
	}()

	logging.Info("AG-UI dedicated listener started",
		"url", l.URL(), "path", r.cfg.Path, "tls", useTLS)
	return l, nil
}
