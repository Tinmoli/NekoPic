//go:build windows

package safefile

import (
	"os"
	"syscall"
)

func disallowed(info os.FileInfo) bool {
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && data.FileAttributes&(syscall.FILE_ATTRIBUTE_REPARSE_POINT|syscall.FILE_ATTRIBUTE_HIDDEN) != 0
}
