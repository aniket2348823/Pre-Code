package main

import "database/sql"

// AI-generated snippet: string concatenation in a SQL query.
func get(db *sql.DB, id string) {
	rows, _ := db.Query("SELECT * FROM users WHERE id = " + id)
	_ = rows
}
