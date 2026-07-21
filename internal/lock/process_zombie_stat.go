package lock

import "strings"

// linuxStatIsZombie reports whether a /proc/<pid>/stat record has state 'Z'.
// Pure string parsing so unit tests can run on any GOOS.
func linuxStatIsZombie(stat string) bool {
	state, ok := parseStatState(stat)
	return ok && state == 'Z'
}

// parseStatState extracts the process state byte from a /proc/<pid>/stat record.
// After the last ')' (end of the comm field) the next token is state
// (R/S/D/Z/T/…). Returns false if the record is malformed.
func parseStatState(stat string) (byte, bool) {
	end := strings.LastIndex(stat, ")")
	if end < 0 || end+1 >= len(stat) {
		return 0, false
	}
	fields := strings.Fields(stat[end+1:])
	if len(fields) < 1 || fields[0] == "" {
		return 0, false
	}
	return fields[0][0], true
}
