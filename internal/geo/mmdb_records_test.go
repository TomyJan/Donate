package geo

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/TomyJan/Donate/internal/config"
)

// The replacement has the same encoded length and a valid MMDB data type,
// but bytes cannot be decoded into the country name used by lookupMMDB.
func mmdbWithInvalidCountryType(t *testing.T) []byte {
	t.Helper()
	data := mustReadDatabase(t, "testdata/synthetic-province.mmdb")
	marker := append([]byte{0x46}, []byte("中国")...)
	if bytes.Count(data, marker) != 1 {
		t.Fatal("unexpected synthetic MMDB country record")
	}
	data[bytes.Index(data, marker)] = 0x86
	return data
}

func TestMMDBValidationRejectsIncompatibleRecordTypes(t *testing.T) {
	for _, tc := range []struct {
		name        string
		marker      []byte
		replacement byte
	}{
		{"country", append([]byte{0x46}, []byte("中国")...), 0x86},
		{"subdivision", append([]byte{0x49}, []byte("合成省")...), 0x89},
		{"accuracy", []byte{0xa1, 10}, 0x81},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := mustReadDatabase(t, "testdata/synthetic-province.mmdb")
			if bytes.Count(data, tc.marker) != 1 {
				t.Fatal("unexpected synthetic MMDB field")
			}
			data[bytes.Index(data, tc.marker)] = tc.replacement
			path := filepath.Join(t.TempDir(), "city.mmdb")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := validateDatabase(path, "mmdb"); err == nil {
				t.Fatal("City metadata and valid binary structure hid an incompatible record")
			}
		})
	}
}

// The checked-in fixture has one 24-bit node, a 16-byte separator and one
// inline record. Give the two halves of IPv4 separate records without adding
// dependencies or using real geolocation data.
func mmdbWithTwoRecords(t *testing.T, second []byte) []byte {
	t.Helper()
	data := mustReadDatabase(t, "testdata/synthetic-province.mmdb")
	metadata := bytes.Index(data, []byte{0xab, 0xcd, 0xef, 'M', 'a', 'x', 'M', 'i', 'n', 'd', '.', 'c', 'o', 'm'})
	if metadata <= 22 || !bytes.Equal(data[:6], []byte{0, 0, 17, 0, 0, 17}) {
		t.Fatal("unexpected synthetic MMDB layout")
	}
	first := data[22:metadata]
	out := append([]byte(nil), data[:metadata]...)
	pointer := 17 + len(first)
	out[3], out[4], out[5] = byte(pointer>>16), byte(pointer>>8), byte(pointer)
	out = append(out, second...)
	return append(out, data[metadata:]...)
}

func TestMMDBValidationChecksLaterRecords(t *testing.T) {
	bad := mmdbWithInvalidCountryType(t)
	metadata := bytes.Index(bad, []byte{0xab, 0xcd, 0xef})
	path := filepath.Join(t.TempDir(), "city.mmdb")
	if err := os.WriteFile(path, mmdbWithTwoRecords(t, bad[22:metadata]), 0600); err != nil {
		t.Fatal(err)
	}
	if err := validateDatabase(path, "mmdb"); err == nil {
		t.Fatal("a healthy first record hid an incompatible later record")
	}
}

func TestMMDBValidationAllowsEmptyAndMissingFields(t *testing.T) {
	for _, empty := range []bool{true, false} {
		name, record := "empty map", []byte{0xe0}
		if !empty {
			name = "missing fields"
			record = []byte{0xe1, 0x43, 'i', 's', 'p', 0x40} // {isp: ""}
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "city.mmdb")
			if err := os.WriteFile(path, mmdbWithTwoRecords(t, record), 0600); err != nil {
				t.Fatal(err)
			}
			if err := validateDatabase(path, "mmdb"); err != nil {
				t.Fatalf("legitimate unknown record rejected: %v", err)
			}
		})
	}
}

func TestMMDBIncompatibleRecordsDegradeAndScheduleRepair(t *testing.T) {
	path := filepath.Join(t.TempDir(), "city.mmdb")
	if err := os.WriteFile(path, mmdbWithInvalidCountryType(t), 0600); err != nil {
		t.Fatal(err)
	}
	c := config.Config{GeoMode: "local", GeoPath: path, GeoMMDBURL: "https://example.invalid/city.mmdb", GeoUpdateDays: 7}
	s, err := New(c, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.libraryReady("mmdb") {
		t.Error("incompatible MMDB was activated at startup")
	}
	if due := NewUpdater(s, c, nil).Due(time.Now()); !slices.Equal(due, []string{"mmdb"}) {
		t.Errorf("repair was not immediately due: %v", due)
	}
	s.HTTP.Transport = transport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ret":200,"data":{"ip":"1.1.1.1","country":"中国","prov":"江苏"}}`))}, nil
	})
	p, err := s.Lookup(context.Background(), "1.1.1.1")
	if err != nil || p.Province != "江苏" || p.Source != "ip9" {
		t.Fatalf("bad local record suppressed temporary online lookup: %+v %v", p, err)
	}
}

func TestMMDBIncompatibleUpdatePreservesWorkingLibrary(t *testing.T) {
	dir := t.TempDir()
	path, stage := filepath.Join(dir, "live.mmdb"), filepath.Join(dir, "stage.mmdb")
	original := mustReadDatabase(t, "testdata/synthetic-province.mmdb")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stage, mmdbWithInvalidCountryType(t), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := New(config.Config{GeoMode: "local", GeoPath: path}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.installDatabase(context.Background(), databaseSource{kind: "mmdb", path: path}, stage); err == nil {
		t.Error("incompatible MMDB update was installed")
	}
	if !bytes.Equal(original, mustReadDatabase(t, path)) {
		t.Error("incompatible update replaced the healthy file")
	}
	p, err := s.Refresh(context.Background(), "1.1.1.1")
	if err != nil || p.Country != "中国" || p.Province != "合成省" {
		t.Fatalf("healthy reader was lost: %+v %v", p, err)
	}
}

func mmdbWithSharedTree(t *testing.T, bits, ipVersion byte) []byte {
	t.Helper()
	data := mustReadDatabase(t, "testdata/synthetic-province.mmdb")
	marker := bytes.Index(data, []byte{0xab, 0xcd, 0xef})
	depth := 32
	if ipVersion == 6 {
		depth = 128
	}
	nodeSize := int(bits) / 4
	tree := make([]byte, depth*nodeSize+16)
	for node := range depth {
		child := uint32(node + 1)
		if node+1 == depth {
			child += 16
		}
		entry := tree[node*nodeSize : (node+1)*nodeSize]
		switch bits {
		case 24:
			entry[0], entry[1], entry[2] = byte(child>>16), byte(child>>8), byte(child)
			copy(entry[3:], entry[:3])
		case 28:
			entry[0], entry[1], entry[2] = byte(child>>16), byte(child>>8), byte(child)
			entry[3] = byte((child>>20)&0xf0 | (child>>24)&0xf)
			copy(entry[4:], entry[:3])
		case 32:
			binary.BigEndian.PutUint32(entry[:4], child)
			binary.BigEndian.PutUint32(entry[4:], child)
		}
	}
	tree = append(tree, data[22:marker]...)
	metadata := append([]byte(nil), data[marker:]...)
	for _, field := range []struct {
		name            string
		typeByte, value byte
	}{{"node_count", 0xc1, byte(depth)}, {"record_size", 0xa1, bits}, {"ip_version", 0xa1, ipVersion}} {
		at := bytes.Index(metadata, []byte(field.name)) + len(field.name)
		if at < len(field.name) || metadata[at] != field.typeByte {
			t.Fatal("unexpected synthetic MMDB metadata")
		}
		metadata[at+1] = field.value
	}
	return append(tree, metadata...)
}

func TestMMDBSharedIndexesValidateWithoutPrefixExpansion(t *testing.T) {
	for _, bits := range []byte{24, 28, 32} {
		for _, version := range []byte{4, 6} {
			t.Run(fmt.Sprintf("%d-bit IPv%d", bits, version), func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "shared.mmdb")
				if err := os.WriteFile(path, mmdbWithSharedTree(t, bits, version), 0600); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if err := validateDatabaseContext(ctx, path, "mmdb"); err != nil {
					t.Fatalf("tiny shared index did not validate within its budget: %v", err)
				}
			})
		}
	}
}

func TestMMDBSharedIndexesRejectIncompatibleRecords(t *testing.T) {
	for _, bits := range []byte{24, 28, 32} {
		t.Run(fmt.Sprintf("%d-bit", bits), func(t *testing.T) {
			data := mmdbWithSharedTree(t, bits, 6)
			marker := append([]byte{0x46}, []byte("中国")...)
			data[bytes.Index(data, marker)] = 0x86
			path := filepath.Join(t.TempDir(), "bad-shared.mmdb")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := validateDatabase(path, "mmdb"); err == nil {
				t.Fatal("shared index hid an incompatible record")
			}
		})
	}
}
