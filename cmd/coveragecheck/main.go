// coveragecheck guards security and consistency boundaries rather than a repository average.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
)

var critical = []struct{ file, name string }{
	{"internal/auth/session.go", "Resolve"},
	{"internal/auth/session.go", "Renew"},
	{"internal/auth/session.go", "RenewIfNeeded"},
	{"internal/auth/session.go", "Logout"},
	{"internal/auth/flow.go", "claimFlow"},
	{"internal/order/order.go", "Create"},
	{"internal/order/observation.go", "Apply"},
	{"internal/payment/signature.go", "verify"},
	{"internal/jobs/queue.go", "Claim"},
	{"internal/jobs/queue.go", "Finish"},
}

func check(report string) error {
	pattern := regexp.MustCompile("(?m)^github.com/TomyJan/Donate/(.*[.]go):[0-9]+:[[:space:]]+([[:alnum:]_]+)[[:space:]]+([0-9.]+)%")
	values := map[string]float64{}
	for _, match := range pattern.FindAllStringSubmatch(report, -1) {
		value, e := strconv.ParseFloat(match[3], 64)
		if e != nil {
			return e
		}
		values[match[1]+":"+match[2]] = value
	}
	for _, target := range critical {
		key := target.file + ":" + target.name
		value, ok := values[key]
		if !ok || value < 80 {
			return fmt.Errorf("critical coverage %s is %.1f%% (present=%t), require >=80%%", key, value, ok)
		}
	}
	return nil
}
func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: coveragecheck <coverprofile>")
		os.Exit(1)
	}
	data, e := exec.Command("go", "tool", "cover", "-func="+os.Args[1]).CombinedOutput()
	if e == nil {
		e = check(string(data))
	}
	if e != nil {
		fmt.Fprintln(os.Stderr, string(data), e)
		os.Exit(1)
	}
	fmt.Println("All critical session, payment and lease functions meet >=80% coverage.")
}
