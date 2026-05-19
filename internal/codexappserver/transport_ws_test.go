package codexappserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWebSocketTransportSendRecv(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("Upgrade() error: %v", err)
			return
		}
		defer conn.Close()

		_, payload, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("ReadMessage() error: %v", err)
			return
		}
		if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			t.Errorf("WriteMessage() error: %v", err)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	transport, err := DialWebSocket(ctx, wsURLFromHTTP(server.URL))
	if err != nil {
		t.Fatalf("DialWebSocket returned error: %v", err)
	}
	defer transport.Close()

	if err := transport.Send(ctx, []byte(`{"method":"ping"}`)); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	got, err := transport.Recv(ctx)
	if err != nil {
		t.Fatalf("Recv returned error: %v", err)
	}
	if string(got) != `{"method":"ping"}` {
		t.Fatalf("Recv payload = %s", string(got))
	}
}

func TestWebSocketTransportRecvReturnsBufferedFrameBeforeError(t *testing.T) {
	t.Parallel()

	transport := &WebSocketTransport{
		recvCh: make(chan recvEvent, 2),
	}
	transport.recvCh <- recvEvent{payload: []byte(`{"method":"ping"}`)}
	transport.recvCh <- recvEvent{err: errors.New("websocket closed")}

	got, err := transport.Recv(context.Background())
	if err != nil {
		t.Fatalf("Recv returned error: %v", err)
	}
	if string(got) != `{"method":"ping"}` {
		t.Fatalf("Recv payload = %s", string(got))
	}
}

func TestWebSocketTransportRecvPreservesLiveProducerOrder(t *testing.T) {
	t.Parallel()

	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)

	for i := 0; i < 200; i++ {
		transport := &WebSocketTransport{
			recvCh: make(chan recvEvent, 2),
		}

		started := make(chan struct{})
		done := make(chan struct{})
		var (
			got []byte
			err error
		)

		go func() {
			close(started)
			got, err = transport.Recv(context.Background())
			close(done)
		}()

		<-started

		go func() {
			transport.recvCh <- recvEvent{payload: []byte(`{"method":"ping"}`)}
			transport.recvCh <- recvEvent{err: errors.New("websocket closed")}
		}()

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("Recv did not return")
		}

		if err != nil {
			t.Fatalf("iteration %d: Recv returned error before frame: %v", i, err)
		}
		if string(got) != `{"method":"ping"}` {
			t.Fatalf("iteration %d: Recv payload = %s", i, string(got))
		}
	}
}

func wsURLFromHTTP(raw string) string {
	return "ws" + strings.TrimPrefix(raw, "http")
}
