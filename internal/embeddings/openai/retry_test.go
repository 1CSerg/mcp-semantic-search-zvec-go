package openai

import (
	"net/http"
	"testing"
	"time"
)

func TestIsRetryableHTTPStatus(t *testing.T) {
	if !isRetryableHTTPStatus(http.StatusInternalServerError) {
		t.Fatal("expected 500 retryable")
	}
	if isRetryableHTTPStatus(http.StatusBadRequest) {
		t.Fatal("expected 400 not retryable")
	}
}

func TestRetryDelay(t *testing.T) {
	base := 500 * time.Millisecond
	if retryDelay(base, 0) != base {
		t.Fatalf("delay=%v", retryDelay(base, 0))
	}
	delay := retryDelay(base, 2)
	if delay < time.Second || delay > 2*time.Second {
		t.Fatalf("delay=%v want in [1s,2s]", delay)
	}
}
