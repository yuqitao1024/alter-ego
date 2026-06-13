package gitcode

import (
	"context"
	"path/filepath"
	"testing"
)

func TestDeliveryStoreMarksProcessedAndRejectsDuplicates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestDeliveryStore(t)
	defer store.Close()

	record := DeliveryRecord{
		DeliveryID: "delivery-001",
		EventUUID:  "event-uuid-001",
		EventType:  "issue.created",
		IssueKey:   "ABC-123",
	}

	inserted, err := store.MarkProcessed(ctx, record)
	if err != nil {
		t.Fatalf("MarkProcessed first insert returned error: %v", err)
	}
	if !inserted {
		t.Fatal("MarkProcessed first insert returned false, want true")
	}

	inserted, err = store.MarkProcessed(ctx, record)
	if err != nil {
		t.Fatalf("MarkProcessed duplicate returned error: %v", err)
	}
	if inserted {
		t.Fatal("MarkProcessed duplicate returned true, want false")
	}
}

func TestDeliveryStorePersistsDeduplicationAcrossReopen(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "deliveries.db")

	store, err := OpenDeliveryStore(path)
	if err != nil {
		t.Fatalf("OpenDeliveryStore returned error: %v", err)
	}

	record := DeliveryRecord{
		DeliveryID: "delivery-002",
		EventUUID:  "event-uuid-002",
		EventType:  "issue.updated",
		IssueKey:   "ABC-456",
	}

	inserted, err := store.MarkProcessed(ctx, record)
	if err != nil {
		t.Fatalf("MarkProcessed returned error: %v", err)
	}
	if !inserted {
		t.Fatal("MarkProcessed returned false, want true")
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	store, err = OpenDeliveryStore(path)
	if err != nil {
		t.Fatalf("OpenDeliveryStore reopen returned error: %v", err)
	}
	defer store.Close()

	inserted, err = store.MarkProcessed(ctx, record)
	if err != nil {
		t.Fatalf("MarkProcessed after reopen returned error: %v", err)
	}
	if inserted {
		t.Fatal("MarkProcessed after reopen returned true, want false")
	}
}

func openTestDeliveryStore(t *testing.T) *DeliveryStore {
	t.Helper()

	store, err := OpenDeliveryStore(filepath.Join(t.TempDir(), "deliveries.db"))
	if err != nil {
		t.Fatalf("OpenDeliveryStore returned error: %v", err)
	}
	return store
}
