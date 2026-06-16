package lock

import "testing"

func TestParseLockPayloadLegacy(t *testing.T) {
	payload, ok := parseLockPayload("12345 1700000000\n")
	if !ok {
		t.Fatal("expected parse ok")
	}
	if payload.PID != 12345 || payload.Heartbeat != 1700000000 || !payload.Legacy {
		t.Fatalf("payload=%+v", payload)
	}
}

func TestParseLockPayloadModern(t *testing.T) {
	payload, ok := parseLockPayload("12345 100 1700000000")
	if !ok {
		t.Fatal("expected parse ok")
	}
	if payload.PID != 12345 || payload.StartTime != 100 || payload.Heartbeat != 1700000000 || payload.Legacy {
		t.Fatalf("payload=%+v", payload)
	}
}

func TestFormatLockPayloadRoundTrip(t *testing.T) {
	line := formatLockPayload(99, 42, 1700000001)
	payload, ok := parseLockPayload(line)
	if !ok || payload.PID != 99 || payload.StartTime != 42 || payload.Heartbeat != 1700000001 {
		t.Fatalf("payload=%+v ok=%v", payload, ok)
	}
}
