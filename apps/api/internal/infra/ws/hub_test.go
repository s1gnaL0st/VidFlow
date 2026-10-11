package infraws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestHubBroadcastsToConnectedUser(t *testing.T) {
	hub := NewHub(nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = hub.Serve(w, r, 42)
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	waitFor(t, func() bool { return hub.Online(42) })
	hub.Broadcast(context.Background(), []int64{42}, []byte(`{"type":"message.created"}`))

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, content, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read broadcast: %v", err)
	}
	if string(content) != `{"type":"message.created"}` {
		t.Fatalf("unexpected payload: %s", content)
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition was not satisfied")
}
