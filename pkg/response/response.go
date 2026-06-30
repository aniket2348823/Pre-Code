package response

import (
	"encoding/json"
	"net/http"
)

type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
	Message string      `json:"message,omitempty"`
}

func JSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func Success(w http.ResponseWriter, status int, data interface{}) {
	JSON(w, status, Response{
		Success: true,
		Data:    data,
	})
}

func Error(w http.ResponseWriter, status int, message string) {
	JSON(w, status, Response{
		Success: false,
		Error:   message,
	})
}

func Created(w http.ResponseWriter, data interface{}) {
	Success(w, http.StatusCreated, data)
}

func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

func NotFound(w http.ResponseWriter, message string) {
	Error(w, http.StatusNotFound, message)
}

func BadRequest(w http.ResponseWriter, message string) {
	Error(w, http.StatusBadRequest, message)
}

func Unauthorized(w http.ResponseWriter, message string) {
	Error(w, http.StatusUnauthorized, message)
}

func Forbidden(w http.ResponseWriter, message string) {
	Error(w, http.StatusForbidden, message)
}

func InternalError(w http.ResponseWriter, message string) {
	Error(w, http.StatusInternalServerError, message)
}
