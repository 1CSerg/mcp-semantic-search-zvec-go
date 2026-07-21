package openai

import (
	"math/rand"
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
	half := delay / 2
	if half <= 0 {
		return delay
	}
	return half + time.Duration(rand.Int63n(int64(half)+1))
}
