package cmd

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/digiogithub/pando/internal/logging"
)

// shutdownWatchdogDelay is how long a long-running command may take to wind
// down after a shutdown request before the watchdog force-exits the process.
const shutdownWatchdogDelay = 6 * time.Second

// notifyShutdown captures SIGINT/SIGTERM for a long-running command (serve,
// app, desktop). Call it before any startup work and defer release right
// after, so release runs after every other deferred cleanup.
//
// Without a handler the default action kills the process on the spot, so a
// stop that lands while the command starts up (opening the database,
// generating the TLS certificate, binding the IPC bus) would skip every
// deferred cleanup: the database would not be closed through the engine
// (leaving its shared log files behind) and the IPC lock file would not be
// removed. With the handler installed, startup steps that wait on ctx give up,
// the command winds down and its deferred cleanups run.
//
// ctx is done when a signal arrives or request is called. From then on a
// watchdog force-exits the process if it is still running after
// shutdownWatchdogDelay, in case something in the graceful-shutdown path
// hangs. name labels the watchdog log line.
func notifyShutdown(name string) (ctx context.Context, request context.CancelFunc, release func()) {
	return watchShutdown(name, shutdownWatchdogDelay, os.Exit, syscall.SIGINT, syscall.SIGTERM)
}

// watchShutdown is notifyShutdown with the watchdog delay, the exit function
// and the signals injectable for tests.
func watchShutdown(name string, delay time.Duration, exit func(int), sigs ...os.Signal) (context.Context, context.CancelFunc, func()) {
	base, request := context.WithCancel(context.Background())
	ctx, stopSignals := signal.NotifyContext(base, sigs...)
	released := make(chan struct{})

	go func() {
		select {
		case <-released:
			return
		case <-ctx.Done():
		}
		// release cancels ctx too; that is a normal return, not a shutdown
		// request, and needs no watchdog.
		select {
		case <-released:
			return
		default:
		}
		time.Sleep(delay)
		logging.Error(name + " shutdown watchdog: forced exit after " + delay.String())
		exit(1)
	}()

	var once sync.Once
	release := func() {
		once.Do(func() {
			close(released)
			stopSignals()
			request()
		})
	}
	return ctx, request, release
}
