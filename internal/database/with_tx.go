package database

import (
	"context"
	"database/sql"
	"fmt"
)

func WithTx(ctx context.Context, db *sql.DB, fn func(ctx context.Context, tx *sql.Tx) error) (err error) {
	tx, err := db.BeginTx(
		ctx,
		&sql.TxOptions{
			Isolation: sql.LevelSerializable,
		})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
		} else if err != nil {
			tx.Rollback()
		}
	}()

	if err := fn(ctx, tx); err != nil {
		return fmt.Errorf("failed to execute fn: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
