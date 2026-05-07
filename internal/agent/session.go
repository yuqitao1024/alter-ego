package agent

import "sync"

type SessionMessage struct {
	Role    string
	Content string
}

type SessionStore struct {
	mu          sync.Mutex
	maxMessages int
	sessions    map[string][]SessionMessage
}

func NewSessionStore(maxMessages int) *SessionStore {
	if maxMessages <= 0 {
		maxMessages = 12
	}
	return &SessionStore{
		maxMessages: maxMessages,
		sessions:    map[string][]SessionMessage{},
	}
}

func (s *SessionStore) Snapshot(key string) []SessionMessage {
	s.mu.Lock()
	defer s.mu.Unlock()

	messages := s.sessions[key]
	out := make([]SessionMessage, len(messages))
	copy(out, messages)
	return out
}

func (s *SessionStore) AppendTurn(key, userText, assistantText string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	messages := append(s.sessions[key],
		SessionMessage{Role: "user", Content: userText},
		SessionMessage{Role: "assistant", Content: assistantText},
	)
	if len(messages) > s.maxMessages {
		messages = messages[len(messages)-s.maxMessages:]
	}
	s.sessions[key] = messages
	return len(messages)
}

func (s *SessionStore) Reset(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.sessions, key)
}

func (s *SessionStore) Count(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.sessions[key])
}
