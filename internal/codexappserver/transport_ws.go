package codexappserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Transport interface {
	Send(ctx context.Context, payload []byte) error
	Recv(ctx context.Context) ([]byte, error)
	Close() error
}

type WebSocketTransport struct {
	conn *websocket.Conn

	sendMu sync.Mutex

	recvCh chan recvEvent

	closeOnce sync.Once
}

type recvEvent struct {
	payload []byte
	err     error
}

func DialWebSocket(ctx context.Context, rawURL string, bearerToken string) (*WebSocketTransport, error) {
	headers := http.Header{}
	if bearerToken != "" {
		headers.Set("Authorization", "Bearer "+bearerToken)
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, rawURL, headers)
	if err != nil {
		return nil, fmt.Errorf("dial websocket: %w", err)
	}

	t := &WebSocketTransport{
		conn:   conn,
		recvCh: make(chan recvEvent, 16),
	}
	go t.readLoop()
	return t, nil
}

func (t *WebSocketTransport) Send(ctx context.Context, payload []byte) error {
	if t == nil || t.conn == nil {
		return errors.New("websocket transport is not configured")
	}

	t.sendMu.Lock()
	defer t.sendMu.Unlock()

	if deadline, ok := ctx.Deadline(); ok {
		if err := t.conn.SetWriteDeadline(deadline); err != nil {
			return fmt.Errorf("set write deadline: %w", err)
		}
	} else {
		if err := t.conn.SetWriteDeadline(time.Time{}); err != nil {
			return fmt.Errorf("clear write deadline: %w", err)
		}
	}

	if err := t.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		return fmt.Errorf("write message: %w", err)
	}
	return nil
}

func (t *WebSocketTransport) Recv(ctx context.Context) ([]byte, error) {
	if t == nil {
		return nil, errors.New("websocket transport is not configured")
	}

	select {
	case event := <-t.recvCh:
		if event.payload != nil {
			return event.payload, nil
		}
		if event.err == nil {
			return nil, errors.New("websocket transport closed")
		}
		return nil, event.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (t *WebSocketTransport) Close() error {
	if t == nil || t.conn == nil {
		return nil
	}

	var err error
	t.closeOnce.Do(func() {
		err = t.conn.Close()
	})
	return err
}

func (t *WebSocketTransport) readLoop() {
	for {
		_, payload, err := t.conn.ReadMessage()
		if err != nil {
			t.recvCh <- recvEvent{err: err}
			return
		}

		frame := append([]byte(nil), payload...)
		t.recvCh <- recvEvent{payload: frame}
	}
}
