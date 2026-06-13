package gitcode

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"

	_ "modernc.org/sqlite"
)

const sqliteBusyTimeout = 5000

type DeliveryRecord struct {
	DeliveryID string
	EventUUID  string
	EventType  string
	IssueKey   string
}

type DeliveryStore struct {
	db *sql.DB
}

func OpenDeliveryStore(path string) (*DeliveryStore, error) {
	db, err := sql.Open("sqlite", sqliteDeliveryStoreDSN(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite delivery store: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	store := &DeliveryStore{db: db}
	if err := store.init(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func sqliteDeliveryStoreDSN(path string) string {
	pragmas := []string{
		fmt.Sprintf("busy_timeout(%d)", sqliteBusyTimeout),
		"journal_mode(WAL)",
		"foreign_keys(ON)",
	}

	if strings.HasPrefix(path, "file:") {
		separator := "?"
		if strings.Contains(path, "?") {
			separator = "&"
		}
		var builder strings.Builder
		builder.WriteString(path)
		builder.WriteString(separator)
		for i, pragma := range pragmas {
			if i > 0 {
				builder.WriteByte('&')
			}
			builder.WriteString("_pragma=")
			builder.WriteString(url.QueryEscape(pragma))
		}
		return builder.String()
	}

	if path == ":memory:" {
		path = "file::memory:"
	}

	u := &url.URL{Scheme: "file", Path: path}
	query := u.Query()
	for _, pragma := range pragmas {
		query.Add("_pragma", pragma)
	}
	u.RawQuery = query.Encode()
	return u.String()
}

func (s *DeliveryStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *DeliveryStore) MarkProcessed(ctx context.Context, record DeliveryRecord) (bool, error) {
	if record.DeliveryID == "" {
		return false, fmt.Errorf("delivery id is required")
	}

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO webhook_deliveries (
			delivery_id,
			event_uuid,
			event_type,
			issue_key
		) VALUES (?, ?, ?, ?)
		ON CONFLICT(delivery_id) DO NOTHING
	`,
		record.DeliveryID,
		record.EventUUID,
		record.EventType,
		record.IssueKey,
	)
	if err != nil {
		return false, fmt.Errorf("mark delivery %q processed: %w", record.DeliveryID, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("mark delivery %q processed: rows affected: %w", record.DeliveryID, err)
	}

	return rowsAffected > 0, nil
}

func (s *DeliveryStore) init(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS webhook_deliveries (
			delivery_id TEXT PRIMARY KEY,
			event_uuid TEXT NOT NULL,
			event_type TEXT NOT NULL,
			issue_key TEXT NOT NULL
		)
	`)
	if err != nil {
		return fmt.Errorf("create webhook_deliveries table: %w", err)
	}

	return nil
}
