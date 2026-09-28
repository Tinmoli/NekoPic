package safefile

import (
	"io/fs"
	"os"
	"strings"
)

func ValidName(name string) bool {
	return name != "" &&
		!strings.HasPrefix(name, ".") && !strings.ContainsAny(name, "/\\\x00:") &&
		!strings.HasSuffix(name, ".") && !strings.HasSuffix(name, " ")
}

// Open 打开图库文件夹内的普通文件。打开前后各检查一次文件身份，
// 即使检查间隙文件被换成链接或目录，也会拒绝。
func Open(root *os.Root, name string) (*os.File, error) {
	if !ValidName(name) {
		return nil, fs.ErrNotExist
	}

	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() || disallowed(before) {
		return nil, fs.ErrNotExist
	}

	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	after, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !after.Mode().IsRegular() || disallowed(after) || !os.SameFile(before, after) {
		f.Close()
		return nil, fs.ErrNotExist
	}

	return f, nil
}
