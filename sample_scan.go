package main

import (
	"crypto/md5"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os/exec"
)

// 1. Hardcoded Secret Vulnerability
const AWSSecretKey = "AKIAIOSFODNN7EXAMPLE"
const HardcodedJWTSecret = "super-secret-password-12345"

type UserHandler struct {
	db *sql.DB
}

// 2. SQL Injection Vulnerability
func (h *UserHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("username")

	// Vulnerable: Direct string concatenation in SQL query
	query := "SELECT id, email, role FROM users WHERE username = '" + username + "'"
	rows, err := h.db.Query(query)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		var email, role string
		rows.Scan(&id, &email, &role)
		fmt.Fprintf(w, "User: %d | %s | %s\n", id, email, role)
	}
}

// 3. Command Injection Vulnerability
func RunUserDiagnostic(w http.ResponseWriter, r *http.Request) {
	targetHost := r.URL.Query().Get("host")

	// Vulnerable: Unsanitized user input passed directly to shell command execution
	cmd := exec.Command("sh", "-c", "ping -c 3 "+targetHost)
	out, err := cmd.CombinedOutput()
	if err != nil {
		http.Error(w, "Command failed: "+string(out), 500)
		return
	}
	w.Write(out)
}

// 4. Weak Cryptography Vulnerability
func HashUserPassword(password string) string {
	// Vulnerable: Using MD5 for password hashing instead of Argon2 or bcrypt
	hash := md5.Sum([]byte(password))
	return fmt.Sprintf("%x", hash)
}

func main() {
	fmt.Println("Vulnerable sample app running...")
	log.Fatal(http.ListenAndServe(":8081", nil))
}
