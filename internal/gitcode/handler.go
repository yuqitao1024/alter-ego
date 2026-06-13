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
	inFlight map[string]*deliveryAttempt
}

type deliveryAttempt struct {
	done chan struct{}
	keys [2]string
}

func NewWebhookHandler(cfg Config, store *DeliveryStore, service SyncService) *WebhookHandler {
	return &WebhookHandler{
		cfg:      cfg,
		store:    store,
		service:  service,
		inFlight: make(map[string]*deliveryAttempt),
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
	keys := deliveryCoordinationKeys(record)

	for {
		processed, err := h.wasProcessed(ctx, record)
		if err != nil {
			return err
		}
		if processed {
			return nil
		}

		done, leader := h.enterDeliveryAttempt(keys)
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
		h.leaveDeliveryAttempt(keys, done)
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

func deliveryCoordinationKeys(record DeliveryRecord) [2]string {
	return [2]string{
		"delivery:" + record.DeliveryID,
		"event:" + record.EventUUID,
	}
}

func (h *WebhookHandler) enterDeliveryAttempt(keys [2]string) (chan struct{}, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, key := range keys {
		if current, ok := h.inFlight[key]; ok {
			return current.done, false
		}
	}

	current := &deliveryAttempt{
		done: make(chan struct{}),
		keys: keys,
	}
	for _, key := range keys {
		h.inFlight[key] = current
	}
	return current.done, true
}

func (h *WebhookHandler) leaveDeliveryAttempt(keys [2]string, done chan struct{}) {
	h.mu.Lock()
	current, ok := h.inFlight[keys[0]]
	if !ok {
		current, ok = h.inFlight[keys[1]]
	}
	if ok && current.done == done {
		for _, key := range current.keys {
			if h.inFlight[key] == current {
				delete(h.inFlight, key)
			}
		}
	}
	h.mu.Unlock()

	if ok && current.done == done {
		close(done)
	}
}
