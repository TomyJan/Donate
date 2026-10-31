package geo

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/TomyJan/Donate/internal/config"
)

// Only the metadata type changes; the search tree and all records remain valid.
func mmdbProduct(t *testing.T, product string) []byte {
	t.Helper()
	data := mustReadDatabase(t, "testdata/synthetic-province.mmdb")
	original := append([]byte{0x4b}, []byte("GeoIP2-City")...)
	if bytes.Count(data, original) != 1 || len(product) >= 29 {
		t.Fatal("unexpected synthetic MMDB metadata")
	}
	return bytes.Replace(data, original, append([]byte{0x40 | byte(len(product))}, []byte(product)...), 1)
}

func TestUnsupportedMMDBProductPreservesWorkingLibrary(t *testing.T) {
	for _, product := range []string{"GeoIP2-ISP", "GeoIP2-Anonymous-IP", "GeoLite2-ASN", "GeoIP2-Country", "Unknown"} {
		t.Run(product, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "live.mmdb")
			original := mustReadDatabase(t, "testdata/synthetic-province.mmdb")
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			data := mmdbProduct(t, product)
			data = bytes.ReplaceAll(data, []byte("country"), []byte("carrier"))
			data = bytes.ReplaceAll(data, []byte("subdivisions"), []byte("subdivisionx"))
			stage := filepath.Join(dir, "stage.mmdb")
			if err := os.WriteFile(stage, data, 0600); err != nil {
				t.Fatal(err)
			}
			s, err := New(config.Config{GeoMode: "local", GeoPath: path}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if err := s.installDatabase(context.Background(), databaseSource{kind: "mmdb", path: path}, stage); err == nil {
				t.Error("incompatible MMDB product was installed")
			}
			if !bytes.Equal(original, mustReadDatabase(t, path)) {
				t.Error("incompatible MMDB replaced the working file")
			}
			if p, err := s.Refresh(context.Background(), "1.1.1.1"); err != nil || p.Country != "中国" || p.Province != "合成省" {
				t.Fatalf("working City lookup lost: %+v, %v", p, err)
			}
		})
	}
}

func TestSupportedMMDBProductsRemainCompatible(t *testing.T) {
	for _, product := range []string{"GeoIP2-City", "GeoLite2-City", "GeoIP2-Enterprise", "DBIP-City-Lite", "DBIP-City", "DBIP-Location", "DBIP-Location-ISP", "DBIP-Full"} {
		t.Run(product, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "city.mmdb")
			if err := os.WriteFile(path, mmdbProduct(t, product), 0600); err != nil {
				t.Fatal(err)
			}
			if err := validateDatabase(path, "mmdb"); err != nil {
				t.Fatal(err)
			}
		})
	}
}
