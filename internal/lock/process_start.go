package lock

// ProcessStartTime returns the unix timestamp at which the process with the
// given pid started, or 0 if it cannot be determined. It is used as an identity
// component together with a pid to defend against pid reuse (a recycled pid
// has a different start time than the original process).
func ProcessStartTime(pid int) int64 {
	return processStartUnix(pid)
}
