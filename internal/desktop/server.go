package desktop

import (
	"context"
	"fmt"
	"net"
	"os"

	"github.com/digiogithub/pando/internal/api"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/logging"
)

// apiServer holds a reference to the embedded HTTP API server started for the
// desktop app.
type apiServer struct {
	url    string
	token  string
	server *api.Server
}

// stop gracefully shuts down the embedded API server.
func (s *apiServer) stop(ctx context.Context) {
	if s.server != nil {
		s.server.Shutdown(ctx) //nolint:errcheck
	}
}

// startAPIServer finds a free port on 127.0.0.1, starts the internal HTTP API
// server on it, and returns an apiServer with its URL and auth token.
func startAPIServer(ctx context.Context) (*apiServer, error) {
	cwd, err := findCWD()
	if err != nil {
		return nil, fmt.Errorf("desktop: get cwd: %w", err)
	}

	// Load config from the working directory.
	if _, err := config.Load(cwd, false); err != nil {
		return nil, fmt.Errorf("desktop: load config: %w", err)
	}

	// Connect to the database.
	conn, err := db.Connect()
	if err != nil {
		return nil, fmt.Errorf("desktop: db connect: %w", err)
	}

	// Find a free port on loopback.
	port, err := freePort()
	if err != nil {
		return nil, fmt.Errorf("desktop: find free port: %w", err)
	}

	cfg := api.ServerConfig{
		Host: "127.0.0.1",
		Port: port,
		DB:   conn,
		CWD:  cwd,
	}

	srv, err := api.NewServer(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("desktop: create api server: %w", err)
	}

	go func() {
		if err := srv.Start(); err != nil {
			logging.Error("desktop API server stopped: %v", err)
		}
	}()

	serverURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	logging.Info("Desktop API server started on %s", serverURL)

	return &apiServer{
		url:    serverURL,
		token:  srv.GetToken(),
		server: srv,
	}, nil
}

// freePort asks the OS for a free TCP port on loopback.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// findCWD returns the working directory. In desktop mode this is the directory
// from which the app was launched, falling back to the user's home directory.
func findCWD() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", fmt.Errorf("cannot determine working directory: %w", err)
		}
		return home, nil
	}
	return cwd, nil
}
