package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestCoverageRejectsMissingOrUncoveredCriticalFunction(t *testing.T) {
	if e := check("total: (statements) 100.0%"); e == nil {
		t.Fatal("missing functions passed")
	}
	var report strings.Builder
	for _, target := range critical {
		fmt.Fprintln(&report, "github.com/TomyJan/Donate/"+target.file+":1:", target.name, "90.0%")
	}
	if e := check(report.String()); e != nil {
		t.Fatal(e)
	}
	fmt.Fprintln(&report, "github.com/TomyJan/Donate/internal/order/observation.go:1: Apply 79.9%")
	if e := check(report.String()); e == nil {
		t.Fatal("insufficient payment coverage passed")
	}
}
