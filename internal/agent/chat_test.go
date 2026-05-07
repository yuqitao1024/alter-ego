package agent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yuqitao1024/alter-ego/internal/channel"
)

type fakeChatClient struct {
	lastReq ChatRequest
	reply   string
	err     error
}

func (f *fakeChatClient) CreateResponse(ctx context.Context, req ChatRequest) (string, error) {
	f.lastReq = req
	return f.reply, f.err
}

func TestChatHandlerReturnsConfigurationMessageWhenLLMIsNotConfigured(t *testing.T) {
	handler := NewChatHandler(Config{}, NewSessionStore(12), nil)
	event := channel.MessageEvent{
		Text: "hello",
		Conversation: channel.Conversation{
			ID:   "oc_1",
			Kind: channel.ConversationDirect,
		},
	}

	reply, err := handler.HandleMessage(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	if reply.Text != "LLM is not configured." {
		t.Fatalf("reply.Text = %q", reply.Text)
	}
}

func TestChatHandlerBuildsPromptFromHistoryAndCurrentMessage(t *testing.T) {
	store := NewSessionStore(12)
	store.AppendTurn("lark:oc_1", "previous user", "previous assistant")
	client := &fakeChatClient{reply: "next assistant"}
	handler := NewChatHandler(Config{APIKey: "sk-test", Model: "gpt-test"}, store, client)
	event := channel.MessageEvent{
		Text:     "current user",
		Platform: "lark",
		Conversation: channel.Conversation{
			ID:   "oc_1",
			Kind: channel.ConversationDirect,
		},
	}

	reply, err := handler.HandleMessage(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	if reply.Text != "next assistant" {
		t.Fatalf("reply.Text = %q", reply.Text)
	}
	if client.lastReq.Model != "gpt-test" {
		t.Fatalf("Model = %q", client.lastReq.Model)
	}
	if len(client.lastReq.Messages) != 4 {
		t.Fatalf("len(Messages) = %d, want 4", len(client.lastReq.Messages))
	}
	if client.lastReq.Messages[0].Role != "developer" || client.lastReq.Messages[0].Content == "" {
		t.Fatalf("system message = %#v", client.lastReq.Messages[0])
	}
	if client.lastReq.Messages[1].Role != "user" || client.lastReq.Messages[1].Content != "previous user" {
		t.Fatalf("history user = %#v", client.lastReq.Messages[1])
	}
	if client.lastReq.Messages[2].Role != "assistant" || client.lastReq.Messages[2].Content != "previous assistant" {
		t.Fatalf("history assistant = %#v", client.lastReq.Messages[2])
	}
	if client.lastReq.Messages[3].Role != "user" || client.lastReq.Messages[3].Content != "current user" {
		t.Fatalf("current user = %#v", client.lastReq.Messages[3])
	}
}

func TestChatHandlerStoresSuccessfulTurn(t *testing.T) {
	store := NewSessionStore(12)
	client := &fakeChatClient{reply: "assistant reply"}
	handler := NewChatHandler(Config{APIKey: "sk-test", Model: "gpt-test"}, store, client)
	event := channel.MessageEvent{
		Text:     "user text",
		Platform: "lark",
		Conversation: channel.Conversation{
			ID:   "oc_1",
			Kind: channel.ConversationDirect,
		},
	}

	_, err := handler.HandleMessage(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	if store.Count("lark:oc_1") != 2 {
		t.Fatalf("Count = %d, want 2", store.Count("lark:oc_1"))
	}
}

func TestChatHandlerHandlesEmptyResponseText(t *testing.T) {
	store := NewSessionStore(12)
	client := &fakeChatClient{reply: "   "}
	handler := NewChatHandler(Config{APIKey: "sk-test", Model: "gpt-test"}, store, client)
	event := channel.MessageEvent{
		Text:     "user text",
		Platform: "lark",
		Conversation: channel.Conversation{
			ID:   "oc_1",
			Kind: channel.ConversationDirect,
		},
	}

	reply, err := handler.HandleMessage(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	if reply.Text != "The model returned an empty response." {
		t.Fatalf("reply.Text = %q", reply.Text)
	}
}

func TestChatHandlerHandlesClientError(t *testing.T) {
	store := NewSessionStore(12)
	client := &fakeChatClient{err: errors.New("boom")}
	handler := NewChatHandler(Config{APIKey: "sk-test", Model: "gpt-test"}, store, client)
	event := channel.MessageEvent{
		Text:     "user text",
		Platform: "lark",
		Conversation: channel.Conversation{
			ID:   "oc_1",
			Kind: channel.ConversationDirect,
		},
	}

	reply, err := handler.HandleMessage(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	if !strings.Contains(reply.Text, "LLM request failed") {
		t.Fatalf("reply.Text = %q", reply.Text)
	}
}

func TestOpenAIProviderParsesOutputText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if r.URL.Path != "/responses" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output_text":"hello from model"}`))
	}))
	defer server.Close()

	client := NewOpenAIProvider(Config{
		APIKey:  "sk-test",
		BaseURL: server.URL,
		Model:   "gpt-test",
	}, server.Client())

	text, err := client.CreateResponse(context.Background(), ChatRequest{
		Model: "gpt-test",
		Messages: []ChatMessage{
			{Role: "developer", Content: "system"},
			{Role: "user", Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("CreateResponse returned error: %v", err)
	}
	if text != "hello from model" {
		t.Fatalf("text = %q", text)
	}
}

func TestGLMProviderParsesChatCompletionsText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer glm-test" {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hello from glm"}}]}`))
	}))
	defer server.Close()

	client := NewGLMProvider(Config{
		APIKey:  "glm-test",
		BaseURL: server.URL,
		Model:   "GLM-5.1",
	}, server.Client())

	text, err := client.CreateResponse(context.Background(), ChatRequest{
		Model: "GLM-5.1",
		Messages: []ChatMessage{
			{Role: "developer", Content: "system"},
			{Role: "user", Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("CreateResponse returned error: %v", err)
	}
	if text != "hello from glm" {
		t.Fatalf("text = %q", text)
	}
}
