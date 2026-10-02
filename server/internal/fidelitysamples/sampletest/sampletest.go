// Package sampletest gives tests the committed frozen set and a way to mutate a
// private copy of it. It is never imported by non-test code.
package sampletest

import (
	"bytes"
	"encoding/json"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	fsamples "github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples"
)

// Dir is the committed fixtures/frozen-samples/v1 directory.
func Dir(t testing.TB) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repository")
	}
	dir := filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "fixtures", "frozen-samples", "v1")
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
		t.Fatalf("frozen sample set missing: %v", err)
	}
	return dir
}

// Copy duplicates the committed set into a temp directory the test may mutate.
func Copy(t testing.TB) string {
	t.Helper()
	src, dst := Dir(t), t.TempDir()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), raw, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

// Resign rewrites the manifest after fn mutated fields, recomputing every file
// digest, pixel digest and the set digest from the files on disk so that only
// the property under test is broken.
func Resign(t testing.TB, dir string, fn func(m *fsamples.Manifest)) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m fsamples.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if fn != nil {
		fn(&m)
	}
	for i := range m.Cases {
		for _, f := range []*fsamples.File{&m.Cases[i].Original, &m.Cases[i].Mask} {
			b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f.Path)))
			if err != nil {
				continue // a deliberately unreadable path keeps its old digests
			}
			f.SHA256 = fsamples.FileSHA256(b)
			if img, err := png.Decode(bytes.NewReader(b)); err == nil {
				f.PixelSHA256 = fsamples.PixelSHA256(img)
			}
		}
	}
	m.SetSHA256 = fsamples.ComputeSetSHA256(m.Cases)
	out, err := json.MarshalIndent(m, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), out, 0o644); err != nil {
		t.Fatal(err)
	}
}
