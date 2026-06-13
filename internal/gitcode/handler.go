package gitcode

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"sync"
)

type SyncService interface {
	ApplyIssueEvent(ctx context.Context, event IssueEvent) error
	ApplyMergeRequestEvent(ctx context.Context, event MergeRequestEvent) error
}

type WebhookHandler struct {
	cfg     Config
	store   *DeliveryStore
	service SyncService

	mu       sync.Mutex
	inFlight map[string]chan struct{}
}

func NewWebhookHandler(cfg Config, store *DeliveryStore, service SyncService) *WebhookHandler {
	return &WebhookHandler{
		cfg:      cfg,
		store:    store,
		service:  service,
		inFlight: make(map[string]chan struct{}),
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

	if err := validateDeliveryRecord(record); err != nil {
		return err
	}
	if h.service == nil {
		return fmt.Errorf("gitcode sync service is not configured")
	}

	return h.processDelivery(ctx, record, func() error {
		return h.service.ApplyIssueEvent(ctx, event)
	})
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

	if err := validateDeliveryRecord(record); err != nil {
		return err
	}
	if len(event.AssociatedIssueKeys) == 0 {
		return h.processDelivery(ctx, record, func() error {
			return nil
		})
	}
	if h.service == nil {
		return fmt.Errorf("gitcode sync service is not configured")
	}
	return h.processDelivery(ctx, record, func() error {
		return h.service.ApplyMergeRequestEvent(ctx, event)
	})
}

func (h *WebhookHandler) processDelivery(ctx context.Context, record DeliveryRecord, attempt func() error) error {
	key := deliveryCoordinationKey(record)

	for {
		processed, err := h.wasProcessed(ctx, record)
		if err != nil {
			return err
		}
		if processed {
			return nil
		}

		done, leader := h.enterDeliveryAttempt(key)
		if !leader {
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		err = attempt()
		if err == nil {
			_, err = h.markProcessed(ctx, record)
		}
		h.leaveDeliveryAttempt(key, done)
		if err != nil {
			return err
		}
		return nil
	}
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

func deliveryCoordinationKey(record DeliveryRecord) string {
	return record.DeliveryID + "\x00" + record.EventUUID
}

func (h *WebhookHandler) enterDeliveryAttempt(key string) (chan struct{}, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if done, ok := h.inFlight[key]; ok {
		return done, false
	}

	done := make(chan struct{})
	h.inFlight[key] = done
	return done, true
}

func (h *WebhookHandler) leaveDeliveryAttempt(key string, done chan struct{}) {
	h.mu.Lock()
	current, ok := h.inFlight[key]
	if ok && current == done {
		delete(h.inFlight, key)
	}
	h.mu.Unlock()

	if ok && current == done {
		close(done)
	}
}
