package codexappserver

import (
	"context"
	"errors"
	"fmt"
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

	recvCh chan []byte
	errCh  chan error

	closeOnce sync.Once
}

func DialWebSocket(ctx context.Context, rawURL string) (*WebSocketTransport, error) {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("dial websocket: %w", err)
	}

	t := &WebSocketTransport{
		conn:   conn,
		recvCh: make(chan []byte, 16),
		errCh:  make(chan error, 1),
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
	case payload := <-t.recvCh:
		if payload == nil {
			return nil, t.recvError()
		}
		return payload, nil
	default:
	}

	select {
	case payload := <-t.recvCh:
		if payload == nil {
			return nil, t.recvError()
		}
		return payload, nil
	case err := <-t.errCh:
		if err == nil {
			err = errors.New("websocket transport closed")
		}
		t.publishError(err)
		return nil, err
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
			t.publishError(err)
			return
		}

		frame := append([]byte(nil), payload...)
		t.recvCh <- frame
	}
}

func (t *WebSocketTransport) publishError(err error) {
	select {
	case t.errCh <- err:
	default:
	}
}

func (t *WebSocketTransport) recvError() error {
	select {
	case err := <-t.errCh:
		if err == nil {
			return errors.New("websocket transport closed")
		}
		t.publishError(err)
		return err
	default:
		return errors.New("websocket transport closed")
	}
}
