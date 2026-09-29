// Package server runs an http.Server until its context is cancelled, then
// shuts it down gracefully. Hooks registered with OnStart and OnStop run
// just before the server starts serving and once it has stopped.
//
// Lifecycle log lines go to the Logger passed to WithLogger, or
// slog.Default() (which logger.Init sets) otherwise.
//
//	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
//	defer stop()
//	srv := server.New(router.New(),
//		server.WithAddr(":8080"),
//		server.OnStart(func(ctx context.Context) error { return db.Ping(ctx) }),
//		server.OnStop(func(ctx context.Context) error { return db.Close() }),
//	)
//	if err := srv.Run(ctx); err != nil {
//		os.Exit(1) // Run has already logged err
//	}
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

// Defaults used when the matching option is not given.
const (
	DefaultAddr              = ":8080"
	DefaultReadHeaderTimeout = 10 * time.Second
	DefaultShutdownTimeout   = 10 * time.Second
)

// Logger is what the server logs through. *slog.Logger satisfies it.
type Logger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// Hook is a callback run at a point in the server's lifecycle.
type Hook func(ctx context.Context) error

// Option configures New.
type Option func(*Server)

// WithAddr sets the TCP address to listen on, e.g. ":8080".
func WithAddr(addr string) Option {
	return func(s *Server) { s.addr = addr }
}

// WithShutdownTimeout bounds how long Run waits for in-flight requests and
// the OnStop hook once its context is cancelled.
func WithShutdownTimeout(d time.Duration) Option {
	return func(s *Server) { s.shutdownTimeout = d }
}

// WithLogger sends the server's log lines to l instead of slog.Default().
// A nil l is ignored.
func WithLogger(l Logger) Option {
	return func(s *Server) {
		if l != nil {
			s.log = l
		}
	}
}

// OnStart sets a hook run after the listener is bound and before the server
// starts serving. It gets Run's context. If it returns an error the server
// never serves and Run returns that error; OnStop is not called.
func OnStart(h Hook) Option {
	return func(s *Server) { s.onStart = h }
}

// OnStop sets a hook run once the server has stopped serving, whether Run's
// context was cancelled or serving failed. It gets a context bounded by the
// shutdown timeout. Its error is returned from Run.
func OnStop(h Hook) Option {
	return func(s *Server) { s.onStop = h }
}

// Server is an http.Server with lifecycle hooks. Create one with New.
type Server struct {
	addr            string
	shutdownTimeout time.Duration
	onStart         Hook
	onStop          Hook
	log             Logger
	http            *http.Server

	mu    sync.Mutex
	bound net.Addr
}

// New returns a Server that serves handler.
func New(handler http.Handler, opts ...Option) *Server {
	s := &Server{
		addr:            DefaultAddr,
		shutdownTimeout: DefaultShutdownTimeout,
	}
	for _, opt := range opts {
		opt(s)
	}
	s.http = &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: DefaultReadHeaderTimeout,
	}
	return s
}

// logger returns the configured Logger, or slog.Default() at call time so
// a logger.Init after New still takes effect.
func (s *Server) logger() Logger {
	if s.log != nil {
		return s.log
	}
	return slog.Default()
}

// Addr returns the address the server is listening on, or nil before Run
// has bound it. Useful with WithAddr(":0").
func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bound
}

// Run listens, calls the OnStart hook, and serves until ctx is cancelled or
// serving fails. It then shuts down gracefully, calls the OnStop hook, and
// returns. A clean shutdown returns nil. Any other error is also logged.
// Run must be called only once.
func (s *Server) Run(ctx context.Context) error {
	if err := s.run(ctx); err != nil {
		s.logger().Error("server failed", "error", err)
		return err
	}
	s.logger().Info("server stopped")
	return nil
}

func (s *Server) run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.addr, err)
	}
	s.mu.Lock()
	s.bound = ln.Addr()
	s.mu.Unlock()

	if s.onStart != nil {
		if err := s.onStart(ctx); err != nil {
			_ = ln.Close()
			return fmt.Errorf("start hook: %w", err)
		}
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- s.http.Serve(ln) }()
	s.logger().Info("server started", "addr", ln.Addr().String())

	var errs []error
	select {
	case err := <-serveErr:
		// Serve never returns ErrServerClosed here: only Shutdown causes it.
		errs = append(errs, fmt.Errorf("serve: %w", err))
		s.logger().Warn("server stopping after serve error")
	case <-ctx.Done():
		s.logger().Info("server stopping", "timeout_ms", s.shutdownTimeout.Milliseconds())
	}

	// Detach from ctx, which is usually already cancelled by now.
	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.shutdownTimeout)
	defer cancel()

	if err := s.http.Shutdown(stopCtx); err != nil {
		errs = append(errs, fmt.Errorf("shutdown: %w", err))
	}
	if s.onStop != nil {
		if err := s.onStop(stopCtx); err != nil {
			errs = append(errs, fmt.Errorf("stop hook: %w", err))
		}
	}
	return errors.Join(errs...)
}
