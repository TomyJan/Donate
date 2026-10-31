package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUmamiCSPOnlyAllowsConfiguredOrigin(t *testing.T) {
	for _, origin := range []string{"", "https://stats.example.com:8443"} {
		t.Run(origin, func(t *testing.T) {
			s, _, _ := setup(t)
			s.Config.UmamiOrigin = origin
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
			csp := w.Header().Get("Content-Security-Policy")
			for _, directive := range []string{"script-src", "connect-src"} {
				want := directive + " 'self'"
				if origin != "" {
					want += " " + origin
				}
				if !strings.Contains(csp, want+";") {
					t.Fatalf("missing scoped %s policy: %s", directive, csp)
				}
			}
			if strings.Contains(csp, "unsafe-eval") || strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
				t.Fatal("analytics weakened the script execution policy")
			}
		})
	}
}
