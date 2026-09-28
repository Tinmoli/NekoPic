// Package logfile 把日志按时间写成文件，单个文件超过大小后压缩保存。
package logfile

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const MaxBytes int64 = 100_000_000

type Writer struct {
	mu     sync.Mutex
	dir    string
	limit  int64
	file   *os.File
	size   int64
	closed bool
}

func New(dir string) (*Writer, error) { return newWriter(dir, MaxBytes) }

func newWriter(dir string, limit int64) (*Writer, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	w := &Writer{dir: dir, limit: limit}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *Writer) open() error {
	// 用独占方式创建日志文件，避免多个实例互相覆盖。
	for i := 0; i < 100; i++ {
		name := time.Now().Format("2006-01-02_15-04-05.000000000") + ".log"
		f, err := os.OpenFile(filepath.Join(w.dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0640)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return err
		}
		w.file, w.size = f, 0
		return nil
	}
	return fmt.Errorf("cannot create a unique log filename")
}

func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, os.ErrClosed
	}
	if w.file == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	if err != nil {
		return n, err
	}
	if w.size >= w.limit {
		path := w.file.Name()
		err = w.file.Close()
		w.file = nil
		if err == nil {
			err = compress(path)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "日志压缩失败，原文件已保留：", path, err)
		}
	}
	return n, err
}

func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func compress(path string) error {
	source, err := os.Open(path)
	if err != nil {
		return err
	}
	defer source.Close()
	temp, err := os.CreateTemp(filepath.Dir(path), ".compress-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())

	archive := gzip.NewWriter(temp)
	archive.Name = filepath.Base(path)
	_, copyErr := io.Copy(archive, source)
	zipErr := archive.Close()
	syncErr := temp.Sync()
	closeErr := temp.Close()
	if err := errors.Join(copyErr, zipErr, syncErr, closeErr); err != nil {
		return err
	}
	// 压缩包完整生成之后才删除原文件。
	if _, err := os.Lstat(path + ".gz"); !os.IsNotExist(err) {
		return fmt.Errorf("archive already exists or cannot be checked")
	}
	if err := os.Rename(temp.Name(), path+".gz"); err != nil {
		return err
	}
	if err := source.Close(); err != nil {
		return err
	}
	return os.Remove(path)
}
