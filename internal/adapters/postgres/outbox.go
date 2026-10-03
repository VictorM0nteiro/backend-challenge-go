package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Event types written to the outbox. The publisher, not built yet, reads these
// rows, so the names are part of the contract with its consumers.
const (
	eventWagerTransactionProcessed = "WagerTransactionProcessed"
	eventWalletBalanceChanged      = "WalletBalanceChanged"
)

// insertOutbox records one event inside the caller's transaction. It commits
// together with the state it describes, so an event exists if and only if the
// change it describes committed.
func insertOutbox(ctx context.Context, tx pgx.Tx, aggregateID uuid.UUID, eventType string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("postgres: encode %s event %w", eventType, err)
	}

	query := `INSERT INTO outbox (id, aggregate_id, event_type, payload) VALUES ($1, $2, $3, $4)`
	if _, err := tx.Exec(ctx, query, uuid.New(), aggregateID, eventType, string(raw)); err != nil {
		return fmt.Errorf("postgres: insert outbox %s: %w", eventType, err)
	}

	return nil
}
