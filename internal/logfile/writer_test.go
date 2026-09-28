package logfile

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestRotationPreservesConcurrentRecords(t *testing.T) {
	dir := t.TempDir()
	w, err := newWriter(dir, 128)
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Go(func() {
			for j := 0; j < 20; j++ {
				if _, err := w.Write([]byte("one record\n")); err != nil {
					t.Error(err)
				}
			}
		})
	}
	group.Wait()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	files, _ := os.ReadDir(dir)
	var count, archives int
	for _, entry := range files {
		path := filepath.Join(dir, entry.Name())
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		var r io.Reader = f
		if strings.HasSuffix(path, ".gz") {
			archives++
			z, err := gzip.NewReader(f)
			if err != nil {
				t.Fatal(err)
			}
			defer z.Close()
			r = z
		}
		data, err := io.ReadAll(r)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		count += strings.Count(string(data), "one record\n")
	}
	if archives == 0 || count != 160 {
		t.Fatalf("archives=%d records=%d", archives, count)
	}
}

func TestCompressionFailureKeepsOriginal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "record.log")
	os.WriteFile(path, []byte("keep this"), 0600)
	os.WriteFile(path+".gz", []byte("existing"), 0600)
	if err := compress(path); err == nil {
		t.Fatal("overwrote archive")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "keep this" {
		t.Fatal("lost original")
	}
}

func TestActualHundredMBThreshold(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	chunk := make([]byte, 1_000_000)
	for i := 0; i < 100; i++ {
		if _, err := w.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.Write([]byte("next file")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	archives, _ := filepath.Glob(filepath.Join(dir, "*.gz"))
	logs, _ := filepath.Glob(filepath.Join(dir, "*.log"))
	if len(archives) != 1 || len(logs) != 1 {
		t.Fatalf("archives=%d logs=%d", len(archives), len(logs))
	}
	f, err := os.Open(archives[0])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	n, err := io.Copy(io.Discard, z)
	if err != nil || n != MaxBytes {
		t.Fatalf("archive bytes=%d error=%v", n, err)
	}
}
