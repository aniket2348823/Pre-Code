package main

import "os"

// Clean snippet: secret read from the environment, never hardcoded.
func main() {
	password := os.Getenv("DB_PASSWORD")
	_ = password
}
