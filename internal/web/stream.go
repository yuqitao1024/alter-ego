package web

import (
	"encoding/json"
	"sync"
	"time"
)

type StreamEvent struct {
	Type      string    `json:"type"`
	TaskID    string    `json:"task_id,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type StreamBroker struct {
	mu      sync.Mutex
	nextID  int
	clients map[int]chan StreamEvent
}

func NewStreamBroker() *StreamBroker {
	return &StreamBroker{
		clients: make(map[int]chan StreamEvent),
	}
}

func (b *StreamBroker) Subscribe() (int, <-chan StreamEvent) {
	if b == nil {
		return 0, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	id := b.nextID
	ch := make(chan StreamEvent, 16)
	b.clients[id] = ch
	return id, ch
}

func (b *StreamBroker) Unsubscribe(id int) {
	if b == nil {
		return
	}
	b.mu.Lock()
	ch, ok := b.clients[id]
	if ok {
		delete(b.clients, id)
	}
	b.mu.Unlock()
	if ok {
		close(ch)
	}
}

func (b *StreamBroker) Publish(event StreamEvent) {
	if b == nil {
		return
	}
	if event.UpdatedAt.IsZero() {
		event.UpdatedAt = time.Now().UTC()
	}

	b.mu.Lock()
	clients := make([]chan StreamEvent, 0, len(b.clients))
	for _, ch := range b.clients {
		clients = append(clients, ch)
	}
	b.mu.Unlock()

	for _, ch := range clients {
		select {
		case ch <- event:
		default:
		}
	}
}

func (e StreamEvent) JSON() []byte {
	payload, err := json.Marshal(e)
	if err != nil {
		return []byte(`{"type":"task_updated"}`)
	}
	return payload
}
