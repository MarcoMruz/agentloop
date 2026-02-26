package errors

import "fmt"

type Category int

const (
	CategoryRetryable   Category = iota
	CategoryFatal
	CategoryUserAbort
	CategoryToolFailure
)

type AgentError struct {
	Category Category
	Message  string
	Cause    error
}

func (e *AgentError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}
func (e *AgentError) Unwrap() error { return e.Cause }

func Retryable(msg string, cause error) *AgentError {
	return &AgentError{Category: CategoryRetryable, Message: msg, Cause: cause}
}
func Fatal(msg string, cause error) *AgentError {
	return &AgentError{Category: CategoryFatal, Message: msg, Cause: cause}
}
func UserAbort(msg string) *AgentError {
	return &AgentError{Category: CategoryUserAbort, Message: msg}
}
func ToolFailure(msg string, cause error) *AgentError {
	return &AgentError{Category: CategoryToolFailure, Message: msg, Cause: cause}
}
func IsRetryable(err error) bool {
	if ae, ok := err.(*AgentError); ok {
		return ae.Category == CategoryRetryable
	}
	return false
}
func IsUserAbort(err error) bool {
	if ae, ok := err.(*AgentError); ok {
		return ae.Category == CategoryUserAbort
	}
	return false
}
