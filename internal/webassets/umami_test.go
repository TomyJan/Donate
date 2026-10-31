package webassets

import (
	"bytes"
	"io"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

func TestUmamiPreparesHTMLOnceWithoutChangingBuiltAssets(t *testing.T) {
	const original = `<!doctype html><html><head><title>Donate</title></head><body><script src="/assets/app-hash.js"></script></body></html>`
	assets := fstest.MapFS{
		"index.html":         {Data: []byte(original)},
		"assets/app-hash.js": {Data: []byte("built application")},
	}
	prepared, err := WithUmami(assets, "https://stats.example/script.js?v=1&site=donate", "a4dca193-c108-47d7-a429-b89c13c7f747")
	if err != nil {
		t.Fatal(err)
	}
	index, err := fs.ReadFile(prepared, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	const tracker = `<script defer src="https://stats.example/script.js?v=1&amp;site=donate" data-website-id="a4dca193-c108-47d7-a429-b89c13c7f747" data-exclude-search="true" data-exclude-hash="true" data-do-not-track="true"></script>`
	if !strings.Contains(string(index), tracker+"</head>") || strings.Count(string(index), "data-website-id=") != 1 {
		t.Fatalf("tracker missing, duplicated or improperly escaped: %s", index)
	}
	if string(assets["index.html"].Data) != original {
		t.Fatal("startup preparation mutated the embedded source")
	}
	assets["index.html"].Data = []byte("changed after startup")
	f, err := prepared.Open("index.html")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	again, err := io.ReadAll(f)
	if err != nil || !bytes.Equal(index, again) {
		t.Fatal("prepared HTML was rebuilt instead of using the startup snapshot", err)
	}
	info, err := f.Stat()
	if err != nil || info.Size() != int64(len(index)) {
		t.Fatal("prepared HTML size is incorrect", err)
	}
	javascript, err := fs.ReadFile(prepared, "assets/app-hash.js")
	if err != nil || string(javascript) != "built application" {
		t.Fatal("built asset changed", err)
	}
}

func TestDisabledUmamiLeavesFrontendUnchanged(t *testing.T) {
	assets := fstest.MapFS{"index.html": {Data: []byte("original")}}
	prepared, err := WithUmami(assets, "", "")
	if err != nil {
		t.Fatal(err)
	}
	index, err := fs.ReadFile(prepared, "index.html")
	if err != nil || string(index) != "original" {
		t.Fatal("disabled analytics altered the frontend", err)
	}
}

func TestUmamiFilesystemViewRemainsConsistent(t *testing.T) {
	assets := fstest.MapFS{
		"index.html":    {Data: []byte("<head></head>")},
		"assets/app.js": {Data: []byte("built application")},
	}
	prepared, err := WithUmami(assets, "https://stats.example/script.js", "a4dca193-c108-47d7-a429-b89c13c7f747")
	if err != nil {
		t.Fatal(err)
	}
	if err = fstest.TestFS(prepared, "index.html", "assets/app.js"); err != nil {
		t.Fatal(err)
	}
}

func TestUmamiDoesNotTreatConfiguredValuesAsHTML(t *testing.T) {
	prepared, err := WithUmami(
		fstest.MapFS{"index.html": {Data: []byte("<head></head>")}},
		`https://stats.example/script.js?value="><img src=x onerror=alert(1)>`,
		`" onload="alert(1)`,
	)
	if err != nil {
		t.Fatal(err)
	}
	index, err := fs.ReadFile(prepared, "index.html")
	if err != nil || bytes.Contains(index, []byte("<img")) || bytes.Contains(index, []byte(`" onload=`)) {
		t.Fatal("configured values escaped their HTML attributes", err)
	}
}

func TestUmamiPreparationFailsClearlyForInvalidFrontend(t *testing.T) {
	for _, assets := range []fs.FS{fstest.MapFS{}, fstest.MapFS{"index.html": {Data: []byte("missing head")}}} {
		if _, err := WithUmami(assets, "https://stats.example/script.js", "website"); err == nil {
			t.Fatal("invalid frontend unexpectedly accepted analytics injection")
		}
	}
	if _, err := WithUmami(fstest.MapFS{}, "https://stats.example/script.js", ""); err == nil {
		t.Fatal("partial analytics configuration was accepted")
	}
}
