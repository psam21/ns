package errors

import (
	"errors"
	"testing"
)

func TestAppErrorWrapPreservesCauseAndMetadata(t *testing.T) {
	cause := errors.New("database unavailable")
	err := Wrap(cause, ErrorTypeDatabase, "DB_DOWN", "database read failed").WithRequestID("req-1").WithDetails("retryable")
	if err.Error() != "[database:DB_DOWN] database read failed: retryable" {
		t.Fatalf("Error() = %q", err.Error())
	}
	if !errors.Is(err, cause) || err.Type != ErrorTypeDatabase || err.Code != "DB_DOWN" || err.RequestID != "req-1" || err.Details != "retryable" {
		t.Fatalf("wrapped error metadata is incorrect: %#v", err)
	}
}

func TestErrorHelpersSetExpectedTypes(t *testing.T) {
	cases := []struct {
		name string
		err  *AppError
		kind ErrorType
	}{
		{"validation", ValidationError("BAD_INPUT", "invalid event"), ErrorTypeValidation},
		{"not found", NotFoundError("event"), ErrorTypeNotFound},
		{"rate limit", RateLimitError("relay"), ErrorTypeRateLimit},
		{"timeout", TimeoutError("query"), ErrorTypeTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.Type != tc.kind || tc.err.Code == "" || tc.err.Message == "" {
				t.Fatalf("unexpected helper error: %#v", tc.err)
			}
		})
	}
}

func TestIsRecoverable(t *testing.T) {
	if !IsRecoverable(TimeoutError("request")) {
		t.Fatal("timeout should be recoverable")
	}
	if IsRecoverable(ValidationError("BAD", "invalid")) {
		t.Fatal("validation error should not be recoverable")
	}
	if IsRecoverable(errors.New("plain error")) {
		t.Fatal("plain errors should not be classified as recoverable")
	}
}
