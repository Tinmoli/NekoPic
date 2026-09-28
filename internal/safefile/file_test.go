package safefile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNamesAndLinks(t *testing.T) {
	rootPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootPath, "valid.png"), []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	f, err := Open(root, "valid.png")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	for _, name := range []string{"", ".", "..", "../valid.png", "dir/valid.png", "dir\\valid.png", "file:stream", ".hidden", "a\x00b", "name.", "name "} {
		if _, err := Open(root, name); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	target := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(target, []byte("outside"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(rootPath, "linked.png")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Open(root, "linked.png"); err == nil {
		t.Fatal("outside symlink served")
	}
	if err := os.Symlink(filepath.Join(rootPath, "valid.png"), filepath.Join(rootPath, "inside.png")); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root, "inside.png"); err == nil {
		t.Fatal("inside symlink served")
	}
}
