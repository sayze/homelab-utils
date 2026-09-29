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

func TestRun_Lifecycle(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name      string
		addrInUse bool
		startErr  error
		stopErr   error
		wantErr   error // Run's error must wrap this
		wantAny   bool  // Run must fail, with any error
		wantCalls []string
		wantLogs  []string
	}{
		{
			name:      "clean run",
			wantCalls: []string{"start", "stop"},
			wantLogs:  []string{"INFO server started", "INFO server stopping", "INFO server stopped"},
		},
		{
			name:      "start hook fails",
			startErr:  boom,
			wantErr:   boom,
			wantCalls: []string{"start"},
			wantLogs:  []string{"ERROR server failed"},
		},
		{
			name:      "stop hook fails",
			stopErr:   boom,
			wantErr:   boom,
			wantCalls: []string{"start", "stop"},
			wantLogs:  []string{"INFO server started", "INFO server stopping", "ERROR server failed"},
		},
		{
			name:      "listen fails",
			addrInUse: true,
			wantAny:   true,
			wantLogs:  []string{"ERROR server failed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr := "127.0.0.1:0"
			if tt.addrInUse {
				ln, err := net.Listen("tcp", addr)
				require.NoError(t, err)
				defer func() { _ = ln.Close() }()
				addr = ln.Addr().String()
			}

			// OnStart cancels ctx, so Run stops as soon as it starts serving.
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			log := &fakeLogger{}
			var calls []string
			s := New(okHandler,
				WithAddr(addr),
				WithLogger(log),
				OnStart(func(context.Context) error {
					calls = append(calls, "start")
					cancel()
					return tt.startErr
				}),
				OnStop(func(ctx context.Context) error {
					calls = append(calls, "stop")
					assert.NoError(t, ctx.Err(), "stop hook context should still be live")
					return tt.stopErr
				}),
			)

			err := s.Run(ctx)

			switch {
			case tt.wantErr != nil:
				require.ErrorIs(t, err, tt.wantErr)
			case tt.wantAny:
				require.Error(t, err)
			default:
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantCalls, calls)
			assert.Equal(t, tt.wantLogs, log.msgs())
			if err != nil {
				assert.Equal(t, err, log.entries[len(log.entries)-1].args["error"])
			}
			if !tt.addrInUse {
				_, dialErr := net.Dial("tcp", s.Addr().String())
				assert.Error(t, dialErr, "listener should be closed")
			}
		})
	}
}

// TestRun_OptionalConfig checks Run works with options left unset or nil.
func TestRun_OptionalConfig(t *testing.T) {
	tests := []struct {
		name string
		opts []Option
	}{
		{"no hooks or logger", nil},
		{"nil logger", []Option{WithLogger(nil)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			opts := append([]Option{WithAddr("127.0.0.1:0")}, tt.opts...)

			assert.NoError(t, New(okHandler, opts...).Run(ctx))
		})
	}
}

func TestRun_Serves(t *testing.T) {
	started := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := New(okHandler,
		WithAddr("127.0.0.1:0"),
		OnStart(func(context.Context) error { close(started); return nil }),
	)
	done := start(ctx, t, s, started)

	resp, err := http.Get("http://" + s.Addr().String())
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	assert.Equal(t, "ok", string(body))

	cancel()
	require.NoError(t, wait(t, done))
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
