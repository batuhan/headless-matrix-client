package errors

import (
	"encoding/json"
	"errors"
	"net/http"
)

type APIError struct {
	Message string `json:"message"`
	Code    string `json:"code"`
	Details any    `json:"details,omitempty"`
	Status  int    `json:"-"`
}

func (e *APIError) Error() string {
	return e.Message
}

func New(status int, code, message string, details any) *APIError {
	return &APIError{Message: message, Code: code, Details: details, Status: status}
}

func Validation(details any) *APIError {
	return New(http.StatusBadRequest, "VALIDATION_ERROR", "Invalid input", details)
}

func Unauthorized(message string) *APIError {
	if message == "" {
		message = "Unauthorized: missing or invalid token"
	}
	return New(http.StatusUnauthorized, "unauthorized", message, nil)
}

func Forbidden(message string) *APIError {
	if message == "" {
		message = "Forbidden"
	}
	return New(http.StatusForbidden, "forbidden", message, nil)
}

func NotFound(message string) *APIError {
	if message == "" {
		message = "Not found"
	}
	return New(http.StatusNotFound, "NOT_FOUND", message, nil)
}

func NotImplemented(message string) *APIError {
	if message == "" {
		message = "Not implemented"
	}
	return New(http.StatusNotImplemented, "NOT_IMPLEMENTED", message, nil)
}

func Internal(err error) *APIError {
	if err == nil {
		return New(http.StatusInternalServerError, "INTERNAL_ERROR", "Internal error", nil)
	}
	return New(http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
}

func Write(w http.ResponseWriter, err error) {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		apiErr = Internal(err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(apiErr.Status)
	_ = json.NewEncoder(w).Encode(apiErr)
}
