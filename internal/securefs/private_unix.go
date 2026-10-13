//go:build !windows

package securefs

import "os"

func EnsurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return os.Chmod(path, 0o700)
}

func RestrictPrivateFile(path string) error {
	return os.Chmod(path, 0o600)
}
