package gitcode

import (
	"context"
	"database/sql"
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
	record := DeliveryRecord{
		DeliveryID: event.DeliveryID,
		EventUUID:  event.EventUUID,
		EventType:  "issue",
		IssueKey:   event.IssueKey,
	}

	processed, err := h.wasProcessed(ctx, record)
	if err != nil || processed {
		return err
	}
	if err := validateDeliveryRecord(record); err != nil {
		return err
	}
	if h.service == nil {
		return fmt.Errorf("gitcode sync service is not configured")
	}
	if err := h.service.ApplyIssueEvent(ctx, event); err != nil {
		return err
	}
	_, err = h.markProcessed(ctx, record)
	return err
}

func (h *WebhookHandler) handleMergeRequestEvent(ctx context.Context, event MergeRequestEvent) error {
	issueKey := ""
	if len(event.AssociatedIssueKeys) > 0 {
		issueKey = event.AssociatedIssueKeys[0]
	}

	record := DeliveryRecord{
		DeliveryID: event.DeliveryID,
		EventUUID:  event.EventUUID,
		EventType:  "merge_request",
		IssueKey:   issueKey,
	}

	processed, err := h.wasProcessed(ctx, record)
	if err != nil || processed {
		return err
	}
	if err := validateDeliveryRecord(record); err != nil {
		return err
	}
	if len(event.AssociatedIssueKeys) == 0 {
		_, err := h.markProcessed(ctx, record)
		return err
	}
	if h.service == nil {
		return fmt.Errorf("gitcode sync service is not configured")
	}
	if err := h.service.ApplyMergeRequestEvent(ctx, event); err != nil {
		return err
	}
	_, err = h.markProcessed(ctx, record)
	return err
}

func (h *WebhookHandler) wasProcessed(ctx context.Context, record DeliveryRecord) (bool, error) {
	if h.store == nil || h.store.db == nil {
		return false, fmt.Errorf("gitcode delivery store is not configured")
	}

	var matched int
	err := h.store.db.QueryRowContext(ctx, `
		SELECT 1
		FROM webhook_deliveries
		WHERE delivery_id = ? OR event_uuid = ?
		LIMIT 1
	`, record.DeliveryID, record.EventUUID).Scan(&matched)
	if err == nil {
		return true, nil
	}
	if err == sql.ErrNoRows {
		return false, nil
	}
	return false, fmt.Errorf("check gitcode delivery %q processed: %w", record.DeliveryID, err)
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

func validateDeliveryRecord(record DeliveryRecord) error {
	if record.DeliveryID == "" {
		return fmt.Errorf("delivery id is required")
	}
	if record.EventUUID == "" {
		return fmt.Errorf("event uuid is required")
	}
	return nil
}
