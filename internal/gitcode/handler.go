package gitcode

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

type SyncService interface {
	ApplyIssueEvent(ctx context.Context, event IssueEvent) error
	ApplyMergeRequestEvent(ctx context.Context, event MergeRequestEvent) error
}

type WebhookHandler struct {
	cfg     Config
	store   *DeliveryStore
	service SyncService
}

func NewWebhookHandler(cfg Config, store *DeliveryStore, service SyncService) *WebhookHandler {
	return &WebhookHandler{
		cfg:     cfg,
		store:   store,
		service: service,
	}
}

func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("read gitcode webhook body: %v", err), http.StatusBadRequest)
		return
	}

	if err := VerifyRequest(h.cfg, r.Header, body); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	event, err := ParseEvent(r.Header, body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	switch event := event.(type) {
	case IssueEvent:
		if err := h.handleIssueEvent(r.Context(), event); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	case MergeRequestEvent:
		if err := h.handleMergeRequestEvent(r.Context(), event); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	default:
		http.Error(w, fmt.Sprintf("unsupported parsed gitcode event %T", event), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *WebhookHandler) handleIssueEvent(ctx context.Context, event IssueEvent) error {
	processed, err := h.markProcessed(ctx, DeliveryRecord{
		DeliveryID: event.DeliveryID,
		EventUUID:  event.EventUUID,
		EventType:  "issue",
		IssueKey:   event.IssueKey,
	})
	if err != nil || !processed {
		return err
	}
	if h.service == nil {
		return fmt.Errorf("gitcode sync service is not configured")
	}
	return h.service.ApplyIssueEvent(ctx, event)
}

func (h *WebhookHandler) handleMergeRequestEvent(ctx context.Context, event MergeRequestEvent) error {
	issueKey := ""
	if len(event.AssociatedIssueKeys) > 0 {
		issueKey = event.AssociatedIssueKeys[0]
	}

	processed, err := h.markProcessed(ctx, DeliveryRecord{
		DeliveryID: event.DeliveryID,
		EventUUID:  event.EventUUID,
		EventType:  "merge_request",
		IssueKey:   issueKey,
	})
	if err != nil || !processed {
		return err
	}
	if len(event.AssociatedIssueKeys) == 0 {
		return nil
	}
	if h.service == nil {
		return fmt.Errorf("gitcode sync service is not configured")
	}
	return h.service.ApplyMergeRequestEvent(ctx, event)
}

func (h *WebhookHandler) markProcessed(ctx context.Context, record DeliveryRecord) (bool, error) {
	if h.store == nil {
		return false, fmt.Errorf("gitcode delivery store is not configured")
	}

	processed, err := h.store.MarkProcessed(ctx, record)
	if err != nil {
		return false, err
	}
	if !processed {
		return false, nil
	}

	return true, nil
}
