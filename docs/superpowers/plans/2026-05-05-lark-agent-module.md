# Lark Agent Module Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the first Go-based Lark assistant integration so Alter Ego can receive Lark messages, run an internal handler, and send text replies back to the same conversation.

**Architecture:** Start with a small Go module and four focused layers: `cmd/alterego` for process startup, `internal/channel` for platform-neutral message contracts, `internal/agent` for the initial stub handler, and `internal/lark` for Lark SDK integration. Keep Lark SDK imports out of `internal/channel` so later channel refactoring stays possible.

**Tech Stack:** Go 1.22+, standard library tests, `github.com/larksuite/oapi-sdk-go/v3` for Lark/Lark API and WebSocket events.

---

## File Structure

- Create: `go.mod`
  - Declares module `github.com/yuqitao1024/alter-ego` and Go version.
- Create: `cmd/alterego/main.go`
  - Loads config, creates the Lark adapter, attaches the stub agent handler, and starts the process.
- Create: `internal/channel/message.go`
  - Defines platform-neutral message types and handler/sender interfaces.
- Create: `internal/agent/stub.go`
  - Provides deterministic first response behavior.
- Create: `internal/agent/stub_test.go`
  - Tests the stub handler.
- Create: `internal/lark/config.go`
  - Parses environment variables into Lark config.
- Create: `internal/lark/config_test.go`
  - Tests config parsing and defaults.
- Create: `internal/lark/access.go`
  - Implements allowlist and mention checks.
- Create: `internal/lark/access_test.go`
  - Tests DM and group access decisions.
- Create: `internal/lark/convert.go`
  - Converts simplified Lark message payloads into `channel.MessageEvent`.
- Create: `internal/lark/convert_test.go`
  - Tests message normalization.
- Create: `internal/lark/sender.go`
  - Sends outgoing text messages through Lark SDK.
- Create: `internal/lark/sender_test.go`
  - Tests reply target selection through a fake message creator.
- Create: `internal/lark/adapter.go`
  - Wires SDK WebSocket event handling to the internal handler.
- Modify: `README.md`
  - Adds minimal local run instructions and required environment variables.

---

### Task 1: Initialize Go Module and Channel Contracts

**Files:**
- Create: `go.mod`
- Create: `internal/channel/message.go`

- [ ] **Step 1: Create Go module**

Create `go.mod`:

```go
module github.com/yuqitao1024/alter-ego

go 1.22
```

- [ ] **Step 2: Define channel contracts**

Create `internal/channel/message.go`:

```go
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

type OutgoingMessage struct {
	Text         string
	Conversation Conversation
}

type Handler interface {
	HandleMessage(ctx context.Context, event MessageEvent) (OutgoingMessage, error)
}

type MessageSender interface {
	SendMessage(ctx context.Context, message OutgoingMessage) error
}
```

- [ ] **Step 3: Run compile check**

Run:

```bash
go test ./...
```

Expected: command succeeds, reporting no test files or successful package compilation.

- [ ] **Step 4: Commit**

```bash
git add go.mod internal/channel/message.go
git commit -m "feat: add channel message contracts"
```

---

### Task 2: Add Stub Agent Handler with TDD

**Files:**
- Create: `internal/agent/stub_test.go`
- Create: `internal/agent/stub.go`

- [ ] **Step 1: Write failing test**

Create `internal/agent/stub_test.go`:

```go
package agent

import (
	"context"
	"testing"

	"github.com/yuqitao1024/alter-ego/internal/channel"
)

func TestStubHandlerRepliesToSameConversation(t *testing.T) {
	handler := NewStubHandler()
	event := channel.MessageEvent{
		Text: "hello",
		Conversation: channel.Conversation{
			ID:   "oc_group",
			Kind: channel.ConversationGroup,
		},
	}

	reply, err := handler.HandleMessage(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}

	if reply.Conversation != event.Conversation {
		t.Fatalf("reply conversation = %#v, want %#v", reply.Conversation, event.Conversation)
	}
	if reply.Text != "Alter Ego received: hello" {
		t.Fatalf("reply text = %q", reply.Text)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/agent
```

Expected: FAIL because `NewStubHandler` is undefined.

- [ ] **Step 3: Implement stub handler**

Create `internal/agent/stub.go`:

```go
package agent

import (
	"context"
	"strings"

	"github.com/yuqitao1024/alter-ego/internal/channel"
)

type StubHandler struct{}

func NewStubHandler() *StubHandler {
	return &StubHandler{}
}

func (h *StubHandler) HandleMessage(ctx context.Context, event channel.MessageEvent) (channel.OutgoingMessage, error) {
	text := strings.TrimSpace(event.Text)
	if text == "" {
		text = "(empty message)"
	}

	return channel.OutgoingMessage{
		Text:         "Alter Ego received: " + text,
		Conversation: event.Conversation,
	}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
go test ./internal/agent
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/stub.go internal/agent/stub_test.go
git commit -m "feat: add stub agent handler"
```

---

### Task 3: Parse Lark Configuration

**Files:**
- Create: `internal/lark/config_test.go`
- Create: `internal/lark/config.go`

- [ ] **Step 1: Write failing tests**

Create `internal/lark/config_test.go`:

```go
package lark

import "testing"

func TestConfigFromEnvParsesDefaultsAndAllowlists(t *testing.T) {
	env := map[string]string{
		"ALTER_EGO_LARK_APP_ID":       "cli_test",
		"ALTER_EGO_LARK_APP_SECRET":   "secret",
		"ALTER_EGO_LARK_ALLOW_USERS":  "ou_1, ou_2",
		"ALTER_EGO_LARK_ALLOW_GROUPS": "oc_1,oc_2",
	}

	cfg, err := ConfigFromMap(env)
	if err != nil {
		t.Fatalf("ConfigFromMap returned error: %v", err)
	}

	if cfg.AppID != "cli_test" || cfg.AppSecret != "secret" {
		t.Fatalf("credentials were not parsed: %#v", cfg)
	}
	if cfg.Domain != "lark" {
		t.Fatalf("domain = %q, want lark", cfg.Domain)
	}
	if !cfg.RequireMention {
		t.Fatal("RequireMention = false, want true by default")
	}
	if !cfg.AllowUsers["ou_1"] || !cfg.AllowUsers["ou_2"] {
		t.Fatalf("allow users not parsed: %#v", cfg.AllowUsers)
	}
	if !cfg.AllowGroups["oc_1"] || !cfg.AllowGroups["oc_2"] {
		t.Fatalf("allow groups not parsed: %#v", cfg.AllowGroups)
	}
}

func TestConfigFromEnvRequiresCredentials(t *testing.T) {
	_, err := ConfigFromMap(map[string]string{})
	if err == nil {
		t.Fatal("ConfigFromMap returned nil error without credentials")
	}
}

func TestConfigFromEnvParsesRequireMentionFalse(t *testing.T) {
	cfg, err := ConfigFromMap(map[string]string{
		"ALTER_EGO_LARK_APP_ID":          "cli_test",
		"ALTER_EGO_LARK_APP_SECRET":      "secret",
		"ALTER_EGO_LARK_REQUIRE_MENTION": "false",
	})
	if err != nil {
		t.Fatalf("ConfigFromMap returned error: %v", err)
	}
	if cfg.RequireMention {
		t.Fatal("RequireMention = true, want false")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/lark
```

Expected: FAIL because `ConfigFromMap` is undefined.

- [ ] **Step 3: Implement config parsing**

Create `internal/lark/config.go`:

```go
package lark

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	AppID          string
	AppSecret      string
	Domain         string
	AllowUsers     map[string]bool
	AllowGroups    map[string]bool
	RequireMention bool
}

func ConfigFromEnv() (Config, error) {
	return ConfigFromMap(map[string]string{
		"ALTER_EGO_LARK_APP_ID":          os.Getenv("ALTER_EGO_LARK_APP_ID"),
		"ALTER_EGO_LARK_APP_SECRET":      os.Getenv("ALTER_EGO_LARK_APP_SECRET"),
		"ALTER_EGO_LARK_DOMAIN":          os.Getenv("ALTER_EGO_LARK_DOMAIN"),
		"ALTER_EGO_LARK_ALLOW_USERS":     os.Getenv("ALTER_EGO_LARK_ALLOW_USERS"),
		"ALTER_EGO_LARK_ALLOW_GROUPS":    os.Getenv("ALTER_EGO_LARK_ALLOW_GROUPS"),
		"ALTER_EGO_LARK_REQUIRE_MENTION": os.Getenv("ALTER_EGO_LARK_REQUIRE_MENTION"),
	})
}

func ConfigFromMap(values map[string]string) (Config, error) {
	cfg := Config{
		AppID:          strings.TrimSpace(values["ALTER_EGO_LARK_APP_ID"]),
		AppSecret:      strings.TrimSpace(values["ALTER_EGO_LARK_APP_SECRET"]),
		Domain:         strings.TrimSpace(values["ALTER_EGO_LARK_DOMAIN"]),
		AllowUsers:     parseCSVSet(values["ALTER_EGO_LARK_ALLOW_USERS"]),
		AllowGroups:    parseCSVSet(values["ALTER_EGO_LARK_ALLOW_GROUPS"]),
		RequireMention: true,
	}

	if cfg.AppID == "" {
		return Config{}, fmt.Errorf("ALTER_EGO_LARK_APP_ID is required")
	}
	if cfg.AppSecret == "" {
		return Config{}, fmt.Errorf("ALTER_EGO_LARK_APP_SECRET is required")
	}
	if cfg.Domain == "" {
		cfg.Domain = "lark"
	}

	switch strings.ToLower(strings.TrimSpace(values["ALTER_EGO_LARK_REQUIRE_MENTION"])) {
	case "", "true", "1", "yes":
		cfg.RequireMention = true
	case "false", "0", "no":
		cfg.RequireMention = false
	default:
		return Config{}, fmt.Errorf("ALTER_EGO_LARK_REQUIRE_MENTION must be true or false")
	}

	return cfg, nil
}

func parseCSVSet(raw string) map[string]bool {
	set := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		value := strings.TrimSpace(part)
		if value != "" {
			set[value] = true
		}
	}
	return set
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/lark
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/lark/config.go internal/lark/config_test.go
git commit -m "feat: parse Lark configuration"
```

---

### Task 4: Implement Access Control

**Files:**
- Create: `internal/lark/access_test.go`
- Create: `internal/lark/access.go`

- [ ] **Step 1: Write failing tests**

Create `internal/lark/access_test.go`:

```go
package lark

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
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/lark
```

Expected: FAIL because `Allowed` is undefined.

- [ ] **Step 3: Implement access checks**

Create `internal/lark/access.go`:

```go
package lark

import "github.com/yuqitao1024/alter-ego/internal/channel"

func Allowed(cfg Config, event channel.MessageEvent) bool {
	if event.Sender.ID == "" || !cfg.AllowUsers[event.Sender.ID] {
		return false
	}

	switch event.Conversation.Kind {
	case channel.ConversationDirect:
		return true
	case channel.ConversationGroup:
		if event.Conversation.ID == "" || !cfg.AllowGroups[event.Conversation.ID] {
			return false
		}
		if cfg.RequireMention && !event.MentionedBot {
			return false
		}
		return true
	default:
		return false
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/lark
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/lark/access.go internal/lark/access_test.go
git commit -m "feat: add Lark access control"
```

---

### Task 5: Normalize Lark Message Events

**Files:**
- Create: `internal/lark/convert_test.go`
- Create: `internal/lark/convert.go`

- [ ] **Step 1: Write failing tests**

Create `internal/lark/convert_test.go`:

```go
package lark

import (
	"testing"

	"github.com/yuqitao1024/alter-ego/internal/channel"
)

func TestConvertDirectTextMessage(t *testing.T) {
	input := IncomingMessage{
		MessageID:        "om_1",
		ChatID:           "oc_direct",
		ChatType:         "p2p",
		SenderOpenID:     "ou_sender",
		Text:             "hello",
		TextWithoutAtBot: "hello",
		IsMention:        false,
	}

	event := NormalizeIncoming(input)

	if event.ID != "om_1" {
		t.Fatalf("ID = %q", event.ID)
	}
	if event.Text != "hello" || event.RawText != "hello" {
		t.Fatalf("text = %q raw = %q", event.Text, event.RawText)
	}
	if event.Conversation.Kind != channel.ConversationDirect {
		t.Fatalf("conversation kind = %q", event.Conversation.Kind)
	}
	if event.Conversation.ID != "oc_direct" {
		t.Fatalf("conversation ID = %q", event.Conversation.ID)
	}
	if event.Sender.ID != "ou_sender" {
		t.Fatalf("sender ID = %q", event.Sender.ID)
	}
	if event.Platform != "lark" {
		t.Fatalf("platform = %q", event.Platform)
	}
}

func TestConvertGroupTextMessageUsesTextWithoutAtBot(t *testing.T) {
	input := IncomingMessage{
		MessageID:        "om_2",
		ChatID:           "oc_group",
		ChatType:         "group",
		SenderOpenID:     "ou_sender",
		Text:             "@bot status",
		TextWithoutAtBot: "status",
		IsMention:        true,
	}

	event := NormalizeIncoming(input)

	if event.Text != "status" {
		t.Fatalf("text = %q, want status", event.Text)
	}
	if event.RawText != "@bot status" {
		t.Fatalf("raw text = %q", event.RawText)
	}
	if event.Conversation.Kind != channel.ConversationGroup {
		t.Fatalf("conversation kind = %q", event.Conversation.Kind)
	}
	if !event.MentionedBot {
		t.Fatal("MentionedBot = false, want true")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/lark
```

Expected: FAIL because `IncomingMessage` and `NormalizeIncoming` are undefined.

- [ ] **Step 3: Implement normalization**

Create `internal/lark/convert.go`:

```go
package lark

import (
	"strings"

	"github.com/yuqitao1024/alter-ego/internal/channel"
)

const platformName = "lark"

type IncomingMessage struct {
	MessageID        string
	ChatID           string
	ChatType         string
	SenderOpenID     string
	Text             string
	TextWithoutAtBot string
	IsMention        bool
}

func NormalizeIncoming(input IncomingMessage) channel.MessageEvent {
	kind := channel.ConversationDirect
	if strings.EqualFold(input.ChatType, "group") {
		kind = channel.ConversationGroup
	}

	text := strings.TrimSpace(input.TextWithoutAtBot)
	if text == "" {
		text = strings.TrimSpace(input.Text)
	}

	return channel.MessageEvent{
		ID:      input.MessageID,
		Text:    text,
		RawText: strings.TrimSpace(input.Text),
		Conversation: channel.Conversation{
			ID:   input.ChatID,
			Kind: kind,
		},
		Sender: channel.Sender{
			ID: input.SenderOpenID,
		},
		MentionedBot: input.IsMention,
		Platform:     platformName,
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/lark
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/lark/convert.go internal/lark/convert_test.go
git commit -m "feat: normalize Lark message events"
```

---

### Task 6: Add Lark Text Sender

**Files:**
- Create: `internal/lark/sender_test.go`
- Create: `internal/lark/sender.go`

- [ ] **Step 1: Write failing tests**

Create `internal/lark/sender_test.go`:

```go
package lark

import (
	"context"
	"testing"

	"github.com/yuqitao1024/alter-ego/internal/channel"
)

type fakeMessageCreator struct {
	receiveIDType string
	receiveID     string
	text          string
}

func (f *fakeMessageCreator) CreateTextMessage(ctx context.Context, receiveIDType, receiveID, text string) error {
	f.receiveIDType = receiveIDType
	f.receiveID = receiveID
	f.text = text
	return nil
}

func TestSenderUsesChatIDForDirectAndGroupConversations(t *testing.T) {
	fake := &fakeMessageCreator{}
	sender := NewSender(fake)

	err := sender.SendMessage(context.Background(), channel.OutgoingMessage{
		Text: "hello",
		Conversation: channel.Conversation{
			ID:   "oc_chat",
			Kind: channel.ConversationDirect,
		},
	})
	if err != nil {
		t.Fatalf("SendMessage returned error: %v", err)
	}

	if fake.receiveIDType != "chat_id" {
		t.Fatalf("receiveIDType = %q, want chat_id", fake.receiveIDType)
	}
	if fake.receiveID != "oc_chat" {
		t.Fatalf("receiveID = %q", fake.receiveID)
	}
	if fake.text != "hello" {
		t.Fatalf("text = %q", fake.text)
	}
}

func TestSenderRejectsEmptyConversationID(t *testing.T) {
	sender := NewSender(&fakeMessageCreator{})

	err := sender.SendMessage(context.Background(), channel.OutgoingMessage{
		Text: "hello",
		Conversation: channel.Conversation{
			Kind: channel.ConversationGroup,
		},
	})
	if err == nil {
		t.Fatal("SendMessage returned nil error for empty conversation ID")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/lark
```

Expected: FAIL because `NewSender` is undefined.

- [ ] **Step 3: Implement sender abstraction and SDK creator**

Create `internal/lark/sender.go`:

```go
package lark

import (
	"context"
	"fmt"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/yuqitao1024/alter-ego/internal/channel"
)

type MessageCreator interface {
	CreateTextMessage(ctx context.Context, receiveIDType, receiveID, text string) error
}

type Sender struct {
	creator MessageCreator
}

func NewSender(creator MessageCreator) *Sender {
	return &Sender{creator: creator}
}

func (s *Sender) SendMessage(ctx context.Context, message channel.OutgoingMessage) error {
	if message.Conversation.ID == "" {
		return fmt.Errorf("conversation ID is required")
	}
	if message.Text == "" {
		return fmt.Errorf("message text is required")
	}
	return s.creator.CreateTextMessage(ctx, larkim.ReceiveIdTypeChatId, message.Conversation.ID, message.Text)
}

type SDKMessageCreator struct {
	client *larkim.Service
}

func NewSDKMessageCreator(client *larkim.Service) *SDKMessageCreator {
	return &SDKMessageCreator{client: client}
}

func (c *SDKMessageCreator) CreateTextMessage(ctx context.Context, receiveIDType, receiveID, text string) error {
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(receiveID).
			MsgType("text").
			Content(larkim.NewMessageTextBuilder().Text(text).Build()).
			Build()).
		Build()

	_, err := c.client.Message.Create(ctx, req)
	return err
}
```

- [ ] **Step 4: Fetch SDK dependency and run tests**

Run:

```bash
go mod tidy
go test ./internal/lark
```

Expected: PASS. `go.mod` and `go.sum` now include `github.com/larksuite/oapi-sdk-go/v3`.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/lark/sender.go internal/lark/sender_test.go
git commit -m "feat: add Lark text sender"
```

---

### Task 7: Wire Lark WebSocket Adapter

**Files:**
- Create: `internal/lark/adapter.go`

- [ ] **Step 1: Implement adapter wiring**

Create `internal/lark/adapter.go`:

```go
package lark

import (
	"context"
	"encoding/json"
	"fmt"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/larksuite/oapi-sdk-go/v3/ws"

	"github.com/yuqitao1024/alter-ego/internal/channel"
)

type Adapter struct {
	cfg     Config
	handler channel.Handler
	sender  channel.MessageSender
	wsClient *ws.Client
}

func NewAdapter(cfg Config, handler channel.Handler) *Adapter {
	apiClient := lark.NewClient(cfg.AppID, cfg.AppSecret, lark.WithOpenBaseUrl(baseURL(cfg.Domain)))
	sender := NewSender(NewSDKMessageCreator(apiClient.Im))

	adapter := &Adapter{
		cfg:     cfg,
		handler: handler,
		sender:  sender,
	}

	eventHandler := dispatcher.NewEventDispatcher("", "")
	eventHandler.OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
		return adapter.handleP2Message(ctx, event)
	})

	adapter.wsClient = ws.NewClient(
		cfg.AppID,
		cfg.AppSecret,
		ws.WithDomain(baseURL(cfg.Domain)),
		ws.WithEventHandler(eventHandler),
		ws.WithLogLevel(larkcore.LogLevelInfo),
	)

	return adapter
}

func (a *Adapter) Start(ctx context.Context) error {
	if a.wsClient == nil {
		return fmt.Errorf("websocket client is not configured")
	}
	return a.wsClient.Start(ctx)
}

func (a *Adapter) handleP2Message(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
	if event == nil || event.Event == nil || event.Event.Message == nil || event.Event.Sender == nil {
		return nil
	}

	message := event.Event.Message
	sender := event.Event.Sender
	if value(message.MessageType) != "text" {
		return nil
	}

	senderOpenID := ""
	if sender.SenderId != nil {
		senderOpenID = value(sender.SenderId.OpenId)
	}

	text := textFromContent(value(message.Content))
	incoming := IncomingMessage{
		MessageID:        value(message.MessageId),
		ChatID:           value(message.ChatId),
		ChatType:         value(message.ChatType),
		SenderOpenID:     senderOpenID,
		Text:             text,
		TextWithoutAtBot: text,
		IsMention:        len(message.Mentions) > 0,
	}

	normalized := NormalizeIncoming(incoming)
	if !Allowed(a.cfg, normalized) {
		return nil
	}

	reply, err := a.handler.HandleMessage(ctx, normalized)
	if err != nil {
		return err
	}
	if reply.Text == "" {
		return nil
	}
	return a.sender.SendMessage(ctx, reply)
}

func textFromContent(raw string) string {
	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return raw
	}
	return payload.Text
}

func value(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}

func baseURL(domain string) string {
	if domain == "lark" {
		return lark.LarkBaseUrl
	}
	return lark.LarkBaseUrl
}
```

- [ ] **Step 2: Run compile check**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/lark/adapter.go
git commit -m "feat: wire Lark websocket adapter"
```

---

### Task 8: Add CLI Entrypoint

**Files:**
- Create: `cmd/alterego/main.go`

- [ ] **Step 1: Implement process startup**

Create `cmd/alterego/main.go`:

```go
package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/yuqitao1024/alter-ego/internal/agent"
	"github.com/yuqitao1024/alter-ego/internal/lark"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := lark.ConfigFromEnv()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	adapter := lark.NewAdapter(cfg, agent.NewStubHandler())
	err = adapter.Start(ctx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
```

- [ ] **Step 2: Run compile check**

Run:

```bash
go test ./...
go build ./cmd/alterego
```

Expected: both commands PASS.

- [ ] **Step 3: Commit**

```bash
git add cmd/alterego/main.go
git commit -m "feat: add Alter Ego command"
```

---

### Task 9: Document Local Lark Setup

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update README**

Modify `README.md` to:

````md
# Alter Ego

Alter Ego is an early-stage AI agent project focused on creating a virtual counterpart of the person who builds and uses it. The goal is to build an agent that can assist with day-to-day work, explore topics of interest, and help investigate the practical boundaries of modern AI systems.

## Lark Assistant

The first integration target is a Lark assistant account. The Go service connects to Lark through WebSocket event subscription, receives text messages, and sends text replies back to the same conversation.

Required environment variables:

```sh
export ALTER_EGO_LARK_APP_ID="cli_xxx"
export ALTER_EGO_LARK_APP_SECRET="xxx"
export ALTER_EGO_LARK_ALLOW_USERS="ou_xxx"
export ALTER_EGO_LARK_ALLOW_GROUPS="oc_xxx"
```

Optional environment variables:

```sh
export ALTER_EGO_LARK_DOMAIN="lark"
export ALTER_EGO_LARK_REQUIRE_MENTION="true"
```

Run locally:

```sh
go run ./cmd/alterego
```

## License

Copyright 2026 yuqitao1024.

This project is licensed under the [Apache License 2.0](LICENSE).
````

- [ ] **Step 2: Verify README renders as Markdown**

Run:

```bash
sed -n '1,120p' README.md
```

Expected: README contains the Lark Assistant section and no broken code fences.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: add Lark assistant setup"
```

---

### Task 10: Final Verification

**Files:**
- Verify all changed files.

- [ ] **Step 1: Run full test suite**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 2: Run build**

Run:

```bash
go build ./cmd/alterego
```

Expected: PASS and local binary `alterego` may be produced.

- [ ] **Step 3: Remove local build artifact if created**

Run:

```bash
rm -f alterego
git status --short
```

Expected: no uncommitted files.

- [ ] **Step 4: Review commit history**

Run:

```bash
git log --oneline -8
```

Expected: shows small commits for contracts, stub handler, config, access control, normalization, sender, adapter, command, and docs.

---

## Plan Self-Review

Spec coverage:

- Bidirectional Lark support is covered by Tasks 6-8.
- WebSocket event subscription is covered by Task 7.
- Internal channel boundary is covered by Task 1.
- Stub handler is covered by Task 2.
- Environment configuration is covered by Task 3.
- Allowlists and group mention safety defaults are covered by Task 4.
- Message normalization is covered by Task 5.
- Tests listed in the design spec are covered by Tasks 2-6 and final verification.
- README setup guidance is covered by Task 9.

No placeholders:

- The plan uses no placeholder markers.
- Each code task includes concrete file paths, code, commands, and expected results.

Type consistency:

- The internal interfaces use `channel.MessageEvent`, `channel.OutgoingMessage`, `channel.Handler`, and `channel.MessageSender` consistently.
- Lark sender code consistently sends to `chat_id`, matching the design's "same conversation" behavior.
