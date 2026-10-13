package ui

import (
	"io/fs"
	"os"
	"testing"
)

func TestEmbedsEveryBuiltFile(t *testing.T) {
	disk := os.DirFS("dist")
	if _, err := fs.Stat(disk, "index.html"); err != nil {
		t.Skip("SPA is not built")
	}
	embedded := Dist()
	if embedded == nil {
		t.Fatal("dist is built but not embedded")
	}
	err := fs.WalkDir(disk, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if _, err := fs.Stat(embedded, p); err != nil {
			t.Errorf("%s is built but not embedded", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
