package apiclient

import "fmt"

type APIError struct {
	StatusCode int
	Code       string
	Message    string
	RequestID  string
}

func (e *APIError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("api %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("api %d %s: %s", e.StatusCode, e.Code, e.Message)
}
