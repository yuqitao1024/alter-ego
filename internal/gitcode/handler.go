package gitcode

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
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

const deliveryClaimPollInterval = 50 * time.Millisecond

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
	for {
		claimResult, err := h.claimDelivery(ctx, record)
		if err != nil {
			return err
		}
		switch claimResult {
		case DeliveryAlreadyProcessed:
			return nil
		case DeliveryInProgress:
			if err := waitForNextClaimAttempt(ctx); err != nil {
				return err
			}
			continue
		case DeliveryClaimed:
			err = attempt()
			if err != nil {
				releaseErr := h.releaseDeliveryClaim(context.WithoutCancel(ctx), record)
				if releaseErr != nil {
					return fmt.Errorf("%w (release delivery claim: %v)", err, releaseErr)
				}
				return err
			}
			if err := h.completeDeliveryClaim(ctx, record); err != nil {
				releaseErr := h.releaseDeliveryClaim(context.WithoutCancel(ctx), record)
				if releaseErr != nil {
					return fmt.Errorf("%w (release delivery claim: %v)", err, releaseErr)
				}
				return err
			}
			return nil
		default:
			return fmt.Errorf("unsupported delivery claim result %d", claimResult)
		}
	}
}

func (h *WebhookHandler) claimDelivery(ctx context.Context, record DeliveryRecord) (DeliveryClaimResult, error) {
	if h.store == nil {
		return DeliveryInProgress, fmt.Errorf("gitcode delivery store is not configured")
	}
	return h.store.TryClaim(ctx, record)
}

func (h *WebhookHandler) completeDeliveryClaim(ctx context.Context, record DeliveryRecord) error {
	if h.store == nil {
		return fmt.Errorf("gitcode delivery store is not configured")
	}
	return h.store.CompleteClaim(ctx, record)
}

func (h *WebhookHandler) releaseDeliveryClaim(ctx context.Context, record DeliveryRecord) error {
	if h.store == nil {
		return fmt.Errorf("gitcode delivery store is not configured")
	}
	return h.store.ReleaseClaim(ctx, record)
}

func waitForNextClaimAttempt(ctx context.Context) error {
	timer := time.NewTimer(deliveryClaimPollInterval)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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
