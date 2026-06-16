package lock

import (
	"strconv"
	"strings"
)

type lockPayload struct {
	PID       int
	StartTime int64
	Heartbeat int64
	Legacy    bool
}

func parseLockPayload(data string) (lockPayload, bool) {
	fields := strings.Fields(strings.TrimSpace(data))
	if len(fields) < 2 {
		return lockPayload{}, false
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil || pid <= 0 {
		return lockPayload{}, false
	}
	if len(fields) >= 3 {
		start, err1 := strconv.ParseInt(fields[1], 10, 64)
		heartbeat, err2 := strconv.ParseInt(fields[2], 10, 64)
		if err1 == nil && err2 == nil {
			return lockPayload{
				PID:       pid,
				StartTime: start,
				Heartbeat: heartbeat,
			}, true
		}
	}
	heartbeat, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return lockPayload{}, false
	}
	return lockPayload{
		PID:       pid,
		Heartbeat: heartbeat,
		Legacy:    true,
	}, true
}

func formatLockPayload(pid int, startTime, heartbeat int64) string {
	return strings.TrimSpace(strings.Join([]string{
		strconv.Itoa(pid),
		strconv.FormatInt(startTime, 10),
		strconv.FormatInt(heartbeat, 10),
	}, " ")) + "\n"
}
