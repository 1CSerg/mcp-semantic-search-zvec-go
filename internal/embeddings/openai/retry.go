package openai

import (
	"net/http"
	"time"
)

func isRetryableHTTPStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func retryDelay(base time.Duration, attempt int) time.Duration {
	if attempt <= 0 {
		return base
	}
	delay := base
	for i := 0; i < attempt; i++ {
		delay *= 2
	}
	return delay
}
