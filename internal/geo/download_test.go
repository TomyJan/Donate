package geo

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestDatabaseArchiveExtraction(t *testing.T) {
	data := []byte("database contents")
	for _, format := range []string{"raw", "gzip", "tar.gz", "zip"} {
		t.Run(format, func(t *testing.T) {
			var encoded bytes.Buffer
			switch format {
			case "raw":
				encoded.Write(data)
			case "gzip":
				w := gzip.NewWriter(&encoded)
				_, _ = w.Write(data)
				_ = w.Close()
			case "tar.gz":
				g := gzip.NewWriter(&encoded)
				w := tar.NewWriter(g)
				_ = w.WriteHeader(&tar.Header{Name: "nested/../../ip2region_v4.xdb", Mode: 0600, Size: int64(len(data))})
				_, _ = w.Write(data)
				_ = w.Close()
				_ = g.Close()
			case "zip":
				w := zip.NewWriter(&encoded)
				f, _ := w.Create("nested/ip2region_v4.xdb")
				_, _ = f.Write(data)
				_ = w.Close()
			}
			dir := t.TempDir()
			src := filepath.Join(dir, "download")
			dst := filepath.Join(dir, "stage")
			if err := os.WriteFile(src, encoded.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			if err := unpackDatabase(src, dst, "xdb4", 4096); err != nil {
				t.Fatal(err)
			}
			actual, err := os.ReadFile(dst)
			if err != nil || !bytes.Equal(actual, data) {
				t.Fatal(string(actual), err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 2 {
				t.Fatal("archive created extra paths", entries, err)
			}
		})
	}
}

func TestDatabaseArchiveRejectsCorruptionAndTooManyEntries(t *testing.T) {
	var compressed bytes.Buffer
	g := gzip.NewWriter(&compressed)
	_, _ = g.Write([]byte("library contents"))
	_ = g.Close()
	brokenGzip := append([]byte(nil), compressed.Bytes()...)
	brokenGzip[len(brokenGzip)-1] ^= 0xff
	var tarData bytes.Buffer
	tarWriter := tar.NewWriter(&tarData)
	_ = tarWriter.WriteHeader(&tar.Header{Name: "ip2region_v4.xdb", Mode: 0600, Size: 1000})
	_, _ = tarWriter.Write([]byte("truncated"))
	var zipData bytes.Buffer
	z := zip.NewWriter(&zipData)
	for i := 0; i <= maxArchiveEntries; i++ {
		_, _ = z.Create(fmt.Sprintf("file-%d.txt", i))
	}
	_ = z.Close()
	for _, data := range [][]byte{brokenGzip, tarData.Bytes(), zipData.Bytes()} {
		dir := t.TempDir()
		path := filepath.Join(dir, "download")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if err := unpackDatabase(path, filepath.Join(dir, "stage"), "xdb4", 1<<20); err == nil {
			t.Fatal("damaged or excessive archive accepted")
		}
	}
}

func TestDatabaseArchiveRejectsAmbiguityAndExpansion(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string][]byte
		limit int64
	}{
		{"ambiguous", map[string][]byte{"first.xdb": []byte("one"), "second.xdb": []byte("two")}, 4096},
		{"wrong family", map[string][]byte{"ip2region_v6.xdb": []byte("ipv6")}, 4096},
		{"expanded", map[string][]byte{"ip2region_v4.xdb": bytes.Repeat([]byte("a"), 1024)}, 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			z := zip.NewWriter(&b)
			for name, body := range tc.files {
				f, _ := z.Create(name)
				_, _ = f.Write(body)
			}
			_ = z.Close()
			dir := t.TempDir()
			src := filepath.Join(dir, "download")
			_ = os.WriteFile(src, b.Bytes(), 0600)
			if err := unpackDatabase(src, filepath.Join(dir, "stage"), "xdb4", tc.limit); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}
