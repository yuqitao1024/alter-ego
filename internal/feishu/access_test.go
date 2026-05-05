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

func TestAccessDeniesEmptySenderID(t *testing.T) {
	cfg := Config{AllowUsers: map[string]bool{"ou_allowed": true}}
	event := channel.MessageEvent{
		Sender: channel.Sender{ID: ""},
		Conversation: channel.Conversation{
			ID:   "ou_allowed",
			Kind: channel.ConversationDirect,
		},
	}

	if Allowed(cfg, event) {
		t.Fatal("Allowed returned true for empty sender ID")
	}
}

func TestAccessDeniesDirectSenderWithNilUserAllowlist(t *testing.T) {
	cfg := Config{AllowUsers: nil}
	event := channel.MessageEvent{
		Sender: channel.Sender{ID: "ou_allowed"},
		Conversation: channel.Conversation{
			ID:   "ou_allowed",
			Kind: channel.ConversationDirect,
		},
	}

	if Allowed(cfg, event) {
		t.Fatal("Allowed returned true with nil user allowlist")
	}
}

func TestAccessDeniesDirectSenderWithEmptyUserAllowlist(t *testing.T) {
	cfg := Config{AllowUsers: map[string]bool{}}
	event := channel.MessageEvent{
		Sender: channel.Sender{ID: "ou_allowed"},
		Conversation: channel.Conversation{
			ID:   "ou_allowed",
			Kind: channel.ConversationDirect,
		},
	}

	if Allowed(cfg, event) {
		t.Fatal("Allowed returned true with empty user allowlist")
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

func TestAccessAllowsKnownGroupWithoutMentionWhenNotRequired(t *testing.T) {
	cfg := Config{
		AllowUsers:     map[string]bool{"ou_allowed": true},
		AllowGroups:    map[string]bool{"oc_allowed": true},
		RequireMention: false,
	}
	event := channel.MessageEvent{
		MentionedBot: false,
		Sender:       channel.Sender{ID: "ou_allowed"},
		Conversation: channel.Conversation{
			ID:   "oc_allowed",
			Kind: channel.ConversationGroup,
		},
	}

	if !Allowed(cfg, event) {
		t.Fatal("Allowed returned false for allowlisted group message when mention is not required")
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

func TestAccessDeniesGroupWithUnknownConversationID(t *testing.T) {
	cfg := Config{
		AllowUsers:  map[string]bool{"ou_allowed": true},
		AllowGroups: map[string]bool{"oc_allowed": true},
	}
	event := channel.MessageEvent{
		MentionedBot: true,
		Sender:       channel.Sender{ID: "ou_allowed"},
		Conversation: channel.Conversation{
			ID:   "oc_other",
			Kind: channel.ConversationGroup,
		},
	}

	if Allowed(cfg, event) {
		t.Fatal("Allowed returned true for non-allowlisted group conversation")
	}
}

func TestAccessDeniesGroupWithEmptyConversationID(t *testing.T) {
	cfg := Config{
		AllowUsers:  map[string]bool{"ou_allowed": true},
		AllowGroups: map[string]bool{"oc_allowed": true},
	}
	event := channel.MessageEvent{
		MentionedBot: true,
		Sender:       channel.Sender{ID: "ou_allowed"},
		Conversation: channel.Conversation{
			ID:   "",
			Kind: channel.ConversationGroup,
		},
	}

	if Allowed(cfg, event) {
		t.Fatal("Allowed returned true for group with empty conversation ID")
	}
}

func TestAccessDeniesUnknownConversationKind(t *testing.T) {
	cfg := Config{AllowUsers: map[string]bool{"ou_allowed": true}}
	event := channel.MessageEvent{
		Sender: channel.Sender{ID: "ou_allowed"},
		Conversation: channel.Conversation{
			ID:   "ou_allowed",
			Kind: channel.ConversationKind("channel"),
		},
	}

	if Allowed(cfg, event) {
		t.Fatal("Allowed returned true for unknown conversation kind")
	}
}
