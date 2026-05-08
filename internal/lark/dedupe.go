package lark

import (
	"sync"
	"time"
)

const defaultMessageDedupeTTL = 10 * time.Minute

type messageDeduper struct {
	mu   sync.Mutex
	ttl  time.Duration
	now  func() time.Time
	seen map[string]time.Time
}

func newMessageDeduper(ttl time.Duration, now func() time.Time) *messageDeduper {
	if ttl <= 0 {
		ttl = defaultMessageDedupeTTL
	}
	if now == nil {
		now = time.Now
	}
	return &messageDeduper{
		ttl:  ttl,
		now:  now,
		seen: make(map[string]time.Time),
	}
}

func (d *messageDeduper) MarkIfNew(messageID string) bool {
	if messageID == "" {
		return true
	}

	now := d.now()

	d.mu.Lock()
	defer d.mu.Unlock()

	for id, ts := range d.seen {
		if now.Sub(ts) > d.ttl {
			delete(d.seen, id)
		}
	}

	if ts, ok := d.seen[messageID]; ok && now.Sub(ts) <= d.ttl {
		return false
	}

	d.seen[messageID] = now
	return true
}
