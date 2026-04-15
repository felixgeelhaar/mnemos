package main

import (
	"database/sql"
	"log"
)

// closeDB closes a sql.DB value, logging any error.
// Use as: defer closeDB(db)
func closeDB(db *sql.DB) {
	if err := db.Close(); err != nil {
		log.Printf("close db: %v", err)
	}
}
