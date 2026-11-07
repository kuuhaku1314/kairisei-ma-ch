//go:build !windows && !linux

package admin

// Unsupported platforms must report unavailable instead of a false zero load.
func processCPUSeconds() *float64 { return nil }
