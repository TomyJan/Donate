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

func TestDownloadRedirectCredentialsStayWithinOriginalOrigin(t *testing.T) {
	data := mustReadDatabase(t, syntheticXDB(t, "1.1.1.1", "中国|广东|深圳|网络|CN"))
	for _, tc := range []struct {
		name, target string
		wantAuth     bool
	}{
		{"same origin", "https://downloads.vendor.test/database.xdb", true},
		{"explicit default port", "https://downloads.vendor.test:443/database.xdb", true},
		{"same host different case", "https://DOWNLOADS.VENDOR.TEST/database.xdb", true},
		{"subdomain", "https://mirror.downloads.vendor.test/database.xdb", false},
		{"subdomain with URL credentials", "https://account:license@mirror.downloads.vendor.test/database.xdb", false},
		{"different port", "https://downloads.vendor.test:8443/database.xdb", false},
		{"unrelated host", "https://other.vendor.test/database.xdb", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := NewUpdater(nil, config.Config{}, nil)
			var initialAuth, redirectedAuth bool
			u.HTTP.Transport = transport(func(r *http.Request) (*http.Response, error) {
				user, password, ok := r.BasicAuth()
				authenticated := ok && user == "account" && password == "license"
				if r.URL.Path == "/latest" {
					initialAuth = authenticated
					return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {tc.target}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
				}
				redirectedAuth = authenticated
				if !tc.wantAuth && (r.Header.Get("Authorization") != "" || r.URL.User != nil) {
					t.Error("cross-origin request retained header or URL credentials")
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(data))), Request: r}, nil
			})
			stage, err := u.download(context.Background(), databaseSource{"xdb4", filepath.Join(t.TempDir(), "live.xdb"), "https://account:license@downloads.vendor.test/latest"})
			if stage != "" {
				defer os.Remove(stage)
			}
			if err != nil {
				t.Fatal(err)
			}
			if !initialAuth || redirectedAuth != tc.wantAuth {
				t.Fatalf("initial authentication=%t, redirected authentication=%t, want=%t", initialAuth, redirectedAuth, tc.wantAuth)
			}
		})
	}
}

func TestDownloadRedirectDoesNotSendReferer(t *testing.T) {
	data := mustReadDatabase(t, syntheticXDB(t, "1.1.1.1", "中国|广东|深圳|网络|CN"))
	for _, tc := range []struct {
		name      string
		locations []string
	}{
		{"same origin", []string{"https://downloads.vendor.test/database.xdb"}},
		{"different host", []string{"https://cdn.vendor.test/database.xdb"}},
		{"subdomain", []string{"https://mirror.downloads.vendor.test/database.xdb"}},
		{"different port", []string{"https://downloads.vendor.test:8443/database.xdb"}},
		{"multiple redirects", []string{
			"https://downloads.vendor.test/signed?token=synthetic-intermediate",
			"https://cdn.vendor.test/signed?signature=synthetic-cdn",
			"https://mirror.vendor.test/database.xdb",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := NewUpdater(nil, config.Config{}, nil)
			var referers []string
			u.HTTP.Transport = transport(func(r *http.Request) (*http.Response, error) {
				hop := len(referers)
				referers = append(referers, r.Header.Get("Referer"))
				if hop == 0 && r.URL.Query().Get("license_key") != "synthetic-initial" {
					t.Error("initial request lost its required download query")
				}
				if hop < len(tc.locations) {
					return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {tc.locations[hop]}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(data))), Request: r}, nil
			})
			stage, err := u.download(context.Background(), databaseSource{"xdb4", filepath.Join(t.TempDir(), "live.xdb"), "https://account:license@downloads.vendor.test/latest?license_key=synthetic-initial"})
			if stage != "" {
				defer os.Remove(stage)
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(referers) != len(tc.locations)+1 {
				t.Fatalf("download did not follow the complete redirect chain: %v", referers)
			}
			for hop, referer := range referers {
				if referer != "" {
					t.Errorf("download hop %d leaked its previous URL in Referer: %q", hop, referer)
				}
			}
			if got := mustReadDatabase(t, stage); string(got) != string(data) {
				t.Fatal("downloaded database differs")
			}
		})
	}
}
