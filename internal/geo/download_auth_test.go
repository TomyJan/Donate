package geo

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/TomyJan/Donate/internal/config"
)

func TestAuthenticatedDownloadRedirects(t *testing.T) {
	data := mustReadDatabase(t, syntheticXDB(t, "1.1.1.1", "中国|广东|深圳|网络|CN"))
	for _, tc := range []struct {
		name, location string
		wantSuccess    bool
	}{
		{"absolute same host", "absolute", true},
		{"relative same host", "relative", true},
		{"different host", "cross-host", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var initialAuthenticated, redirectedAuthenticated atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				user, password, ok := r.BasicAuth()
				authenticated := ok && user == "account" && password == "license"
				if r.URL.Path == "/latest" {
					initialAuthenticated.Store(authenticated)
					target := "/database.xdb"
					if tc.location != "relative" {
						host := r.Host
						if tc.location == "cross-host" {
							host = strings.Replace(host, "127.0.0.1", "localhost", 1)
						}
						target = "http://" + host + target
					}
					http.Redirect(w, r, target, http.StatusFound)
					return
				}
				redirectedAuthenticated.Store(authenticated)
				if !authenticated {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				_, _ = w.Write(data)
			}))
			defer server.Close()
			u := NewUpdater(nil, config.Config{Development: true}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			address := strings.Replace(server.URL, "http://", "http://account:license@", 1) + "/latest"
			stage, err := u.download(context.Background(), databaseSource{"xdb4", filepath.Join(t.TempDir(), "live.xdb"), address})
			if stage != "" {
				defer os.Remove(stage)
			}
			if !initialAuthenticated.Load() {
				t.Fatal("initial download request lost Basic authentication")
			}
			if tc.wantSuccess {
				if err != nil {
					t.Fatalf("authenticated redirect failed: %v", err)
				}
				if got := mustReadDatabase(t, stage); string(got) != string(data) {
					t.Fatal("downloaded database differs")
				}
			} else if err == nil || redirectedAuthenticated.Load() {
				t.Fatal("credentials must not reach a different host")
			}
		})
	}
}
