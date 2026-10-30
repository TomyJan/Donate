package geo

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/TomyJan/Donate/internal/config"
)

// synthetic-city.mmdb contains only a fabricated city record. Its location
// accuracy_radius uses the MMDB uint16 type, as standard GeoIP City databases do.
func TestLocalCityUint16AccuracyRadius(t *testing.T) {
	s, err := New(config.Config{GeoMode: "local", GeoPath: "testdata/synthetic-city.mmdb"}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.Lookup(context.Background(), "1.1.1.1")
	if err != nil {
		t.Fatalf("valid synthetic city record: %v", err)
	}
	if p.Country != "中国" || p.Source != "mmdb" || p.Latitude == nil || *p.Latitude != 31.2 || p.Longitude == nil || *p.Longitude != 121.5 || p.AccuracyRadius == nil || *p.AccuracyRadius != 10 {
		t.Fatalf("decoded city record: %+v", p)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err = json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body["accuracy_radius"] != float64(10) {
		t.Fatalf("JSON radius: %s", raw)
	}
}
