package feishu

import (
	"testing"

	"github.com/yuqitao1024/alter-ego/internal/channel"
)

func TestAccessAllowsKnownDirectSender(t *testing.T) {
	cfg := Config{AllowUsers: map[string]bool{"ou_allowed": true}}
	event := channel.MessageEvent{
		Sender: channel.Sender{ID: "ou_allowed"},
		Conversation: channel.Conversation{
			ID:   "ou_allowed",
			Kind: channel.ConversationDirect,
		},
	}

	if !Allowed(cfg, event) {
		t.Fatal("Allowed returned false for allowlisted direct sender")
	}
}

func TestAccessDeniesUnknownDirectSender(t *testing.T) {
	cfg := Config{AllowUsers: map[string]bool{"ou_allowed": true}}
	event := channel.MessageEvent{
		Sender: channel.Sender{ID: "ou_other"},
		Conversation: channel.Conversation{
			ID:   "ou_other",
			Kind: channel.ConversationDirect,
		},
	}

	if Allowed(cfg, event) {
		t.Fatal("Allowed returned true for unknown direct sender")
	}
}

func TestAccessAllowsMentionedKnownGroup(t *testing.T) {
	cfg := Config{
		AllowUsers:     map[string]bool{"ou_allowed": true},
		AllowGroups:    map[string]bool{"oc_allowed": true},
		RequireMention: true,
	}
	event := channel.MessageEvent{
		MentionedBot: true,
		Sender:       channel.Sender{ID: "ou_allowed"},
		Conversation: channel.Conversation{
			ID:   "oc_allowed",
			Kind: channel.ConversationGroup,
		},
	}

	if !Allowed(cfg, event) {
		t.Fatal("Allowed returned false for allowlisted mentioned group message")
	}
}

func TestAccessDeniesGroupWithoutMentionWhenRequired(t *testing.T) {
	cfg := Config{
		AllowUsers:     map[string]bool{"ou_allowed": true},
		AllowGroups:    map[string]bool{"oc_allowed": true},
		RequireMention: true,
	}
	event := channel.MessageEvent{
		MentionedBot: false,
		Sender:       channel.Sender{ID: "ou_allowed"},
		Conversation: channel.Conversation{
			ID:   "oc_allowed",
			Kind: channel.ConversationGroup,
		},
	}

	if Allowed(cfg, event) {
		t.Fatal("Allowed returned true for group message without mention")
	}
}
