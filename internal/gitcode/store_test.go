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
	sameDeliveryIDDifferentEvent := DeliveryRecord{
		DeliveryID: "delivery-001",
		EventUUID:  "event-uuid-002",
		EventType:  "issue.created",
		IssueKey:   "ABC-123",
	}
	sameEventUUIDDifferentDelivery := DeliveryRecord{
		DeliveryID: "delivery-002",
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

	inserted, err = store.MarkProcessed(ctx, sameDeliveryIDDifferentEvent)
	if err != nil {
		t.Fatalf("MarkProcessed same delivery id returned error: %v", err)
	}
	if inserted {
		t.Fatal("MarkProcessed same delivery id returned true, want false")
	}

	inserted, err = store.MarkProcessed(ctx, sameEventUUIDDifferentDelivery)
	if err != nil {
		t.Fatalf("MarkProcessed same event uuid returned error: %v", err)
	}
	if inserted {
		t.Fatal("MarkProcessed same event uuid returned true, want false")
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

func TestDeliveryStoreSharesPersistentStateAcrossConnections(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "deliveries.db")

	store1, err := OpenDeliveryStore(path)
	if err != nil {
		t.Fatalf("OpenDeliveryStore store1 returned error: %v", err)
	}
	defer store1.Close()

	record := DeliveryRecord{
		DeliveryID: "delivery-003",
		EventUUID:  "event-uuid-003",
		EventType:  "issue.deleted",
		IssueKey:   "ABC-789",
	}

	inserted, err := store1.MarkProcessed(ctx, record)
	if err != nil {
		t.Fatalf("MarkProcessed store1 returned error: %v", err)
	}
	if !inserted {
		t.Fatal("MarkProcessed store1 returned false, want true")
	}

	store2, err := OpenDeliveryStore(path)
	if err != nil {
		t.Fatalf("OpenDeliveryStore store2 returned error: %v", err)
	}
	defer store2.Close()

	inserted, err = store2.MarkProcessed(ctx, DeliveryRecord{
		DeliveryID: "delivery-004",
		EventUUID:  "event-uuid-003",
		EventType:  "issue.deleted",
		IssueKey:   "ABC-789",
	})
	if err != nil {
		t.Fatalf("MarkProcessed store2 returned error: %v", err)
	}
	if inserted {
		t.Fatal("MarkProcessed store2 returned true, want false")
	}

	info, err := store2.db.QueryContext(ctx, `PRAGMA database_list`)
	if err != nil {
		t.Fatalf("PRAGMA database_list returned error: %v", err)
	}
	defer info.Close()
	if !info.Next() {
		t.Fatal("PRAGMA database_list returned no rows")
	}
	var seq int
	var name, file string
	if err := info.Scan(&seq, &name, &file); err != nil {
		t.Fatalf("database_list scan returned error: %v", err)
	}
	if file == "" {
		t.Fatal("database file path is empty, want file-backed SQLite database")
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
