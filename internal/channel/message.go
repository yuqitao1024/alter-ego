package channel

import "context"

type ConversationKind string

const (
	ConversationDirect ConversationKind = "direct"
	ConversationGroup  ConversationKind = "group"
)

type Conversation struct {
	ID   string
	Kind ConversationKind
}

type Sender struct {
	ID   string
	Name string
}

type MessageEvent struct {
	ID           string
	Text         string
	RawText      string
	Conversation Conversation
	Sender       Sender
	MentionedBot bool
	Platform     string
}

type CardMessage struct {
	Payload interface{}
}

type OutgoingMessage struct {
	Text         string
	Conversation Conversation
	Card         *CardMessage
}

type CardActionEvent struct {
	ID           string
	Action       string
	Value        map[string]interface{}
	Conversation Conversation
	Sender       Sender
	Platform     string
}

type CardActionResponse struct {
	ToastText string
	Message   *OutgoingMessage
}

type Handler interface {
	HandleMessage(ctx context.Context, event MessageEvent) (OutgoingMessage, error)
}

type MessageSender interface {
	SendMessage(ctx context.Context, message OutgoingMessage) error
}
