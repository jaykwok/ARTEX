package mcphttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCircuitCancellationFailureAndRecovery(t *testing.T) {
	c := &circuit{sem: make(chan struct{}, 1), threshold: 2, cooldown: time.Minute}
	release, err := c.enter(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.enter(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	release(context.Canceled)
	for range 2 {
		done, err := c.enter(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		done(errors.New("transport down"))
	}
	if _, err := c.enter(t.Context()); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("not open: %v", err)
	}
	c.mu.Lock()
	c.until = time.Now().Add(-time.Second)
	c.mu.Unlock()
	probe, err := c.enter(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.enter(t.Context()); !errors.Is(err, ErrCircuitOpen) {
		t.Fatal("multiple recovery probes")
	}
	probe(nil)
	done, err := c.enter(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	done(&rpcError{Code: -32602, Message: "bad args"})
	if c.failures != 0 {
		t.Fatal("application errors must not trip server health")
	}
}
func TestCircuitSharedAcrossClients(t *testing.T) {
	a := serverCircuit(t.Name(), "http://example.invalid", map[string]string{"Authorization": "a"})
	b := serverCircuit(t.Name(), "http://example.invalid", map[string]string{"Authorization": "a"})
	if a != b {
		t.Fatal("server gate is not shared")
	}
	if a == serverCircuit(t.Name(), "http://example.invalid", map[string]string{"Authorization": "b"}) {
		t.Fatal("different credentials share health")
	}
}
func TestSSEHandshakeHonorsCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	_, err := NewSSE(ctx, "stalled", srv.URL, nil, false)
	if err == nil {
		t.Fatal("stalled handshake succeeded")
	}
}
