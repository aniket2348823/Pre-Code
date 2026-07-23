package main

import (
	"fmt"
	"net/http"
	"errors"
	"io/ioutil"
)

// UserHandler processes user requests without proper error handling
func UserHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("id")
	
	// Issue: No validation of userID
	data := fetchUserData(userID)
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(data))
}

// fetchUserData makes an HTTP request with no timeout or retry logic
func fetchUserData(userID string) string {
	resp, err := http.Get("https://api.example.com/users/" + userID)
	if err != nil {
		// Issue: Silent error handling
		return "{}"
	}
	
	// Issue: No defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)
	return string(body)
}

// ProcessConfig reads sensitive data without proper validation
func ProcessConfig(configPath string) error {
	// Issue: No path traversal prevention
	data, err := ioutil.ReadFile(configPath)
	if err != nil {
		// Issue: Error shadowing previous error
		err = errors.New("failed to read")
	}
	
	fmt.Println(string(data))
	return nil
}

// ConcurrentOperation has race condition and goroutine leak risks
func ConcurrentOperation(items []string) {
	for _, item := range items {
		// Issue: Unbounded goroutines, no WaitGroup, no channel cleanup
		go func() {
			result := processItem(item)
			fmt.Println(result)
		}()
	}
}

func processItem(item string) string {
	return item
}

// DatabaseConnection lacks proper resource management
func DatabaseConnection() {
	db := ConnectDB()
	// Issue: No defer db.Close()
	data := db.Query("SELECT * FROM users")
	fmt.Println(data)
}

// main entry point
func main() {
	http.HandleFunc("/user", UserHandler)
	http.ListenAndServe(":8080", nil)
}

func ConnectDB() *DBConn {
	return &DBConn{}
}

type DBConn struct{}

func (d *DBConn) Query(sql string) string {
	return "result"
}

func (d *DBConn) Close() error {
	return nil
}
