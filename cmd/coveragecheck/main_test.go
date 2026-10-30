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

func TestCoverageRejectsUncoveredSessionRenewalWindow(t *testing.T) {
	var report strings.Builder
	for _, target := range critical {
		fmt.Fprintln(&report, "github.com/TomyJan/Donate/"+target.file+":1:", target.name, "90.0%")
	}
	fmt.Fprintln(&report, "github.com/TomyJan/Donate/internal/auth/session.go:1: RenewIfNeeded 79.9%")
	if err := check(report.String()); err == nil || !strings.Contains(err.Error(), "RenewIfNeeded") {
		t.Fatalf("uncovered session renewal implementation passed the gate: %v", err)
	}
}
