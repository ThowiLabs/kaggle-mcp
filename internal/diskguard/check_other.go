//go:build !linux

package diskguard

func Check(path string) error { return nil }
