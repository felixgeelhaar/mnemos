package sqlite

import (
	"database/sql"
	"errors"
	"log"
)

// closeRows closes a sql.Rows value, logging any error.
// Use as: defer closeRows(rows)
func closeRows(rows *sql.Rows) {
	if err := rows.Close(); err != nil {
		log.Printf("close rows: %v", err)
	}
}

// rollbackTx rolls back a sql.Tx, logging any error that is not
// caused by the transaction already being committed.
// Use as: defer rollbackTx(tx)
func rollbackTx(tx *sql.Tx) {
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		log.Printf("rollback tx: %v", err)
	}
}

// closeDB closes a sql.DB value, logging any error.
// Intended for use in tests: defer closeDB(db)
func closeDB(db *sql.DB) {
	if err := db.Close(); err != nil {
		log.Printf("close db: %v", err)
	}
}
