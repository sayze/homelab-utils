package server

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var okHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	_, _ = io.WriteString(w, "ok")
})

// entry is one call recorded by fakeLogger.
type entry struct {
	level, msg string
	args       map[string]any
}

// fakeLogger is safe for concurrent use: Run logs from its own goroutine.
type fakeLogger struct {
	mu      sync.Mutex
	entries []entry
}

func (f *fakeLogger) record(level, msg string, args []any) {
	m := map[string]any{}
	for i := 0; i+1 < len(args); i += 2 {
		m[args[i].(string)] = args[i+1]
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, entry{level, msg, m})
}

func (f *fakeLogger) Info(msg string, args ...any)  { f.record("INFO", msg, args) }
func (f *fakeLogger) Warn(msg string, args ...any)  { f.record("WARN", msg, args) }
func (f *fakeLogger) Error(msg string, args ...any) { f.record("ERROR", msg, args) }

func (f *fakeLogger) msgs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, e := range f.entries {
		out = append(out, e.level+" "+e.msg)
	}
	return out
}

// start runs s in the background and returns once OnStart has fired, plus
// a channel that receives Run's result.
func start(ctx context.Context, t *testing.T, s *Server, started <-chan struct{}) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	select {
	case <-started:
	case err := <-done:
		t.Fatalf("Run returned before starting: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not start")
	}
	return done
}

func wait(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
		return nil
	}
}

func TestRun_HooksAndServing(t *testing.T) {
	var calls []string
	started := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := New(okHandler,
		WithAddr("127.0.0.1:0"),
		OnStart(func(context.Context) error {
			calls = append(calls, "start")
			close(started)
			return nil
		}),
		OnStop(func(ctx context.Context) error {
			calls = append(calls, "stop")
			assert.NoError(t, ctx.Err(), "stop hook context should still be live")
			return nil
		}),
	)
	done := start(ctx, t, s, started)

	resp, err := http.Get("http://" + s.Addr().String())
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	assert.Equal(t, "ok", string(body))

	cancel()
	require.NoError(t, wait(t, done))
	assert.Equal(t, []string{"start", "stop"}, calls)
}

func TestRun_StartHookErrorAborts(t *testing.T) {
	boom := errors.New("boom")
	stopped := false
	s := New(okHandler,
		WithAddr("127.0.0.1:0"),
		OnStart(func(context.Context) error { return boom }),
		OnStop(func(context.Context) error { stopped = true; return nil }),
	)

	err := s.Run(context.Background())

	require.ErrorIs(t, err, boom)
	assert.False(t, stopped, "stop hook should not run when start fails")
	_, dialErr := net.Dial("tcp", s.Addr().String())
	assert.Error(t, dialErr, "listener should be closed")
}

func TestRun_StopHookErrorReturned(t *testing.T) {
	boom := errors.New("boom")
	started := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())

	s := New(okHandler,
		WithAddr("127.0.0.1:0"),
		OnStart(func(context.Context) error { close(started); return nil }),
		OnStop(func(context.Context) error { return boom }),
	)
	done := start(ctx, t, s, started)
	cancel()

	assert.ErrorIs(t, wait(t, done), boom)
}

func TestRun_ListenError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	started := false
	s := New(okHandler,
		WithAddr(ln.Addr().String()),
		OnStart(func(context.Context) error { started = true; return nil }),
	)

	assert.Error(t, s.Run(context.Background()))
	assert.False(t, started, "start hook should not run when listen fails")
}

func TestRun_NoHooks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.NoError(t, New(okHandler, WithAddr("127.0.0.1:0")).Run(ctx))
}

func TestRun_WaitsForInFlightRequests(t *testing.T) {
	inHandler := make(chan struct{})
	release := make(chan struct{})
	slow := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(inHandler)
		<-release
		_, _ = io.WriteString(w, "done")
	})
	started := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())

	s := New(slow,
		WithAddr("127.0.0.1:0"),
		OnStart(func(context.Context) error { close(started); return nil }),
	)
	done := start(ctx, t, s, started)

	respCh := make(chan string, 1)
	go func() {
		resp, err := http.Get("http://" + s.Addr().String())
		if err != nil {
			respCh <- err.Error()
			return
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		respCh <- string(b)
	}()
	<-inHandler
	cancel()

	select {
	case <-done:
		t.Fatal("Run returned while a request was in flight")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)

	assert.Equal(t, "done", <-respCh)
	assert.NoError(t, wait(t, done))
}

func TestWithLogger_Lifecycle(t *testing.T) {
	log := &fakeLogger{}
	started := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())

	s := New(okHandler,
		WithAddr("127.0.0.1:0"),
		WithLogger(log),
		OnStart(func(context.Context) error { close(started); return nil }),
	)
	done := start(ctx, t, s, started)
	cancel()
	require.NoError(t, wait(t, done))

	assert.Equal(t, []string{
		"INFO server started",
		"INFO server stopping",
		"INFO server stopped",
	}, log.msgs())
	assert.Equal(t, s.Addr().String(), log.entries[0].args["addr"])
}

func TestWithLogger_ErrorLogged(t *testing.T) {
	log := &fakeLogger{}
	boom := errors.New("boom")
	s := New(okHandler,
		WithAddr("127.0.0.1:0"),
		WithLogger(log),
		OnStart(func(context.Context) error { return boom }),
	)

	err := s.Run(context.Background())

	require.Len(t, log.entries, 1)
	assert.Equal(t, "server failed", log.entries[0].msg)
	assert.Equal(t, err, log.entries[0].args["error"])
}

func TestWithLogger_NilKeepsDefault(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.NotPanics(t, func() {
		_ = New(okHandler, WithAddr("127.0.0.1:0"), WithLogger(nil)).Run(ctx)
	})
}
