package gitcode

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const sqliteBusyTimeout = 5000

type DeliveryRecord struct {
	DeliveryID string
	EventUUID  string
	EventType  string
	IssueKey   string
}

type DeliveryClaimResult int

const (
	DeliveryClaimed DeliveryClaimResult = iota
	DeliveryAlreadyProcessed
	DeliveryInProgress
)

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
	if err := validateStoreRecord(s, record); err != nil {
		return false, err
	}

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO webhook_deliveries (
			delivery_id,
			event_uuid,
			event_type,
			issue_key,
			processed_at
		) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING
	`,
		record.DeliveryID,
		record.EventUUID,
		record.EventType,
		record.IssueKey,
		time.Now().UTC().Format(time.RFC3339Nano),
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

func (s *DeliveryStore) WasProcessed(ctx context.Context, record DeliveryRecord) (bool, error) {
	if err := validateStoreRecord(s, record); err != nil {
		return false, err
	}

	processedAt, found, err := s.lookupProcessedAt(ctx, record)
	if err != nil {
		return false, err
	}
	return found && processedAt.Valid && processedAt.String != "", nil
}

func (s *DeliveryStore) TryClaim(ctx context.Context, record DeliveryRecord) (DeliveryClaimResult, error) {
	if err := validateStoreRecord(s, record); err != nil {
		return DeliveryInProgress, err
	}

	for {
		result, err := s.db.ExecContext(ctx, `
			INSERT INTO webhook_deliveries (
				delivery_id,
				event_uuid,
				event_type,
				issue_key,
				processed_at
			) VALUES (?, ?, ?, ?, NULL)
			ON CONFLICT DO NOTHING
		`,
			record.DeliveryID,
			record.EventUUID,
			record.EventType,
			record.IssueKey,
		)
		if err != nil {
			return DeliveryInProgress, fmt.Errorf("claim delivery %q: %w", record.DeliveryID, err)
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return DeliveryInProgress, fmt.Errorf("claim delivery %q: rows affected: %w", record.DeliveryID, err)
		}
		if rowsAffected > 0 {
			return DeliveryClaimed, nil
		}

		processedAt, found, err := s.lookupProcessedAt(ctx, record)
		if err != nil {
			return DeliveryInProgress, err
		}
		if !found {
			continue
		}
		if processedAt.Valid && processedAt.String != "" {
			return DeliveryAlreadyProcessed, nil
		}
		return DeliveryInProgress, nil
	}
}

func (s *DeliveryStore) CompleteClaim(ctx context.Context, record DeliveryRecord) error {
	if err := validateStoreRecord(s, record); err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE webhook_deliveries
		SET processed_at = ?
		WHERE (delivery_id = ? OR event_uuid = ?)
		  AND processed_at IS NULL
	`,
		time.Now().UTC().Format(time.RFC3339Nano),
		record.DeliveryID,
		record.EventUUID,
	)
	if err != nil {
		return fmt.Errorf("complete delivery %q claim: %w", record.DeliveryID, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("complete delivery %q claim: rows affected: %w", record.DeliveryID, err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("complete delivery %q claim: no in-progress delivery", record.DeliveryID)
	}
	return nil
}

func (s *DeliveryStore) ReleaseClaim(ctx context.Context, record DeliveryRecord) error {
	if err := validateStoreRecord(s, record); err != nil {
		return err
	}

	_, err := s.db.ExecContext(ctx, `
		DELETE FROM webhook_deliveries
		WHERE (delivery_id = ? OR event_uuid = ?)
		  AND processed_at IS NULL
	`,
		record.DeliveryID,
		record.EventUUID,
	)
	if err != nil {
		return fmt.Errorf("release delivery %q claim: %w", record.DeliveryID, err)
	}
	return nil
}

func validateStoreRecord(s *DeliveryStore, record DeliveryRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("gitcode delivery store is not configured")
	}
	if record.DeliveryID == "" {
		return fmt.Errorf("delivery id is required")
	}
	if record.EventUUID == "" {
		return fmt.Errorf("event uuid is required")
	}
	return nil
}

func (s *DeliveryStore) lookupProcessedAt(ctx context.Context, record DeliveryRecord) (sql.NullString, bool, error) {
	var processedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT processed_at
		FROM webhook_deliveries
		WHERE delivery_id = ? OR event_uuid = ?
		LIMIT 1
	`, record.DeliveryID, record.EventUUID).Scan(&processedAt)
	if err == nil {
		return processedAt, true, nil
	}
	if err == sql.ErrNoRows {
		return sql.NullString{}, false, nil
	}
	return sql.NullString{}, false, fmt.Errorf("check gitcode delivery %q processed: %w", record.DeliveryID, err)
}

func (s *DeliveryStore) init(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delivery store init transaction: %w", err)
	}

	rollback := func() {
		_ = tx.Rollback()
	}

	_, err = tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS webhook_deliveries (
			delivery_id TEXT PRIMARY KEY,
			event_uuid TEXT NOT NULL,
			event_type TEXT NOT NULL,
			issue_key TEXT NOT NULL,
			processed_at TEXT
		)
	`)
	if err != nil {
		rollback()
		return fmt.Errorf("create webhook_deliveries table: %w", err)
	}

	hasProcessedAt, err := tableHasColumn(ctx, tx, "webhook_deliveries", "processed_at")
	if err != nil {
		rollback()
		return err
	}
	if !hasProcessedAt {
		if _, err := tx.ExecContext(ctx, `
			ALTER TABLE webhook_deliveries
			ADD COLUMN processed_at TEXT
		`); err != nil {
			rollback()
			return fmt.Errorf("add webhook_deliveries processed_at column: %w", err)
		}
	}

	_, err = tx.ExecContext(ctx, `
		DELETE FROM webhook_deliveries
		WHERE rowid NOT IN (
			SELECT MIN(rowid)
			FROM webhook_deliveries
			GROUP BY event_uuid
		)
	`)
	if err != nil {
		rollback()
		return fmt.Errorf("dedupe historical webhook deliveries: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		CREATE UNIQUE INDEX IF NOT EXISTS webhook_deliveries_event_uuid_idx
		ON webhook_deliveries(event_uuid)
	`)
	if err != nil {
		rollback()
		return fmt.Errorf("create webhook_deliveries event uuid index: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE webhook_deliveries
		SET processed_at = COALESCE(processed_at, ?)
	`, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		rollback()
		return fmt.Errorf("backfill webhook_deliveries processed_at column: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delivery store init transaction: %w", err)
	}

	return nil
}

func tableHasColumn(ctx context.Context, tx *sql.Tx, tableName, columnName string) (bool, error) {
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%s)`, tableName))
	if err != nil {
		return false, fmt.Errorf("inspect %s schema: %w", tableName, err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return false, fmt.Errorf("scan %s schema: %w", tableName, err)
		}
		if name == columnName {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("read %s schema: %w", tableName, err)
	}
	return false, nil
}
