package admin

import "syscall"

func processCPUSeconds() *float64 {
	var usage syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &usage) != nil {
		return nil
	}
	seconds := float64(usage.Utime.Sec) + float64(usage.Stime.Sec) +
		(float64(usage.Utime.Usec)+float64(usage.Stime.Usec))/1e6
	return &seconds
}
