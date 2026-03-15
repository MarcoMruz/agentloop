package errors

import (
	"fmt"
	"testing"
)

func TestRetryableCategory(t *testing.T) {
	cause := fmt.Errorf("connection lost")
	err := Retryable("network error", cause)
	if err.Category != CategoryRetryable {
		t.Fatalf("expected CategoryRetryable, got %d", err.Category)
	}
	if err.Cause != cause {
		t.Fatal("expected cause to be preserved")
	}
}

func TestFatalCategory(t *testing.T) {
	cause := fmt.Errorf("bad config")
	err := Fatal("invalid config", cause)
	if err.Category != CategoryFatal {
		t.Fatalf("expected CategoryFatal, got %d", err.Category)
	}
}

func TestUserAbortCategory(t *testing.T) {
	err := UserAbort("user cancelled")
	if err.Category != CategoryUserAbort {
		t.Fatalf("expected CategoryUserAbort, got %d", err.Category)
	}
	if err.Cause != nil {
		t.Fatal("UserAbort should have nil cause")
	}
}

func TestToolFailureCategory(t *testing.T) {
	cause := fmt.Errorf("exit code 1")
	err := ToolFailure("bash failed", cause)
	if err.Category != CategoryToolFailure {
		t.Fatalf("expected CategoryToolFailure, got %d", err.Category)
	}
}

func TestErrorStringWithCause(t *testing.T) {
	cause := fmt.Errorf("timeout")
	err := Retryable("connection failed", cause)
	expected := "connection failed: timeout"
	if err.Error() != expected {
		t.Fatalf("expected %q, got %q", expected, err.Error())
	}
}

func TestErrorStringWithoutCause(t *testing.T) {
	err := UserAbort("cancelled")
	if err.Error() != "cancelled" {
		t.Fatalf("expected %q, got %q", "cancelled", err.Error())
	}
}

func TestUnwrapReturnsCause(t *testing.T) {
	cause := fmt.Errorf("root cause")
	err := Fatal("wrapped", cause)
	if err.Unwrap() != cause {
		t.Fatal("Unwrap should return the original cause")
	}
}

func TestUnwrapNilWhenNoCause(t *testing.T) {
	err := UserAbort("no cause")
	if err.Unwrap() != nil {
		t.Fatal("Unwrap should return nil when no cause")
	}
}

func TestIsRetryableTrue(t *testing.T) {
	err := Retryable("retry me", nil)
	if !IsRetryable(err) {
		t.Fatal("expected IsRetryable to return true for retryable error")
	}
}

func TestIsRetryableFalseForOtherCategories(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"fatal", Fatal("fatal", nil)},
		{"user_abort", UserAbort("abort")},
		{"tool_failure", ToolFailure("fail", nil)},
	}
	for _, tt := range tests {
		if IsRetryable(tt.err) {
			t.Fatalf("IsRetryable should be false for %s", tt.name)
		}
	}
}

func TestIsRetryableFalseForStdError(t *testing.T) {
	err := fmt.Errorf("plain error")
	if IsRetryable(err) {
		t.Fatal("IsRetryable should be false for standard errors")
	}
}

func TestIsUserAbortTrue(t *testing.T) {
	err := UserAbort("user cancelled")
	if !IsUserAbort(err) {
		t.Fatal("expected IsUserAbort to return true for user abort error")
	}
}

func TestIsUserAbortFalseForOtherCategories(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"retryable", Retryable("retry", nil)},
		{"fatal", Fatal("fatal", nil)},
		{"tool_failure", ToolFailure("fail", nil)},
	}
	for _, tt := range tests {
		if IsUserAbort(tt.err) {
			t.Fatalf("IsUserAbort should be false for %s", tt.name)
		}
	}
}

func TestIsUserAbortFalseForStdError(t *testing.T) {
	err := fmt.Errorf("plain error")
	if IsUserAbort(err) {
		t.Fatal("IsUserAbort should be false for standard errors")
	}
}

func TestAgentErrorImplementsErrorInterface(t *testing.T) {
	var err error = Retryable("test", nil)
	if err.Error() != "test" {
		t.Fatalf("expected %q, got %q", "test", err.Error())
	}
}
