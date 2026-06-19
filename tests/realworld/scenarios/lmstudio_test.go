//go:build realworld && zvec

package scenarios

import (
	"net/http"
	"os"
	"testing"
	"time"
)

func TestLMStudioDownSkip(t *testing.T) {
	if os.Getenv("REALWORLD_PROFILE") != "lmstudio" {
		t.Skip("LM Studio tier only (run via make test-realworld-lmstudio)")
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:1234/v1/models")
	if err == nil {
		resp.Body.Close()
		t.Skip("LM Studio is up; use TestLMStudioSemanticRanking instead")
	}
	t.Log("LM Studio unreachable — suite correctly skipped when server down")
}
