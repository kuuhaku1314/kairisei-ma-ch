package admin

import "syscall"

func processCPUSeconds() *float64 {
	process, err := syscall.GetCurrentProcess()
	if err != nil {
		return nil
	}
	var created, exited, kernel, user syscall.Filetime
	if syscall.GetProcessTimes(process, &created, &exited, &kernel, &user) != nil {
		return nil
	}
	// Filetime.Nanoseconds converts an absolute date, not these CPU durations.
	ticks := func(value syscall.Filetime) float64 {
		return float64(uint64(value.HighDateTime)<<32 | uint64(value.LowDateTime))
	}
	seconds := (ticks(kernel) + ticks(user)) / 1e7
	return &seconds
}
