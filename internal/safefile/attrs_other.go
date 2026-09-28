//go:build !windows

package safefile

import "os"

func disallowed(info os.FileInfo) bool { return false }
