package main

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

func TestFrontendAssetsFromFSRequiresBothEntrypoints(t *testing.T) {
	tests := []struct {
		name        string
		files       fstest.MapFS
		wantMissing string
	}{
		{
			name:        "missing WebUI",
			files:       fstest.MapFS{},
			wantMissing: webIndexPath,
		},
		{
			name: "missing Zashboard",
			files: fstest.MapFS{
				webIndexPath: {Data: []byte("web")},
			},
			wantMissing: zashboardIndexPath,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := frontendAssetsFromFS(test.files)
			if err == nil {
				t.Fatalf("expected missing asset error for %q", test.wantMissing)
			}
			if !strings.Contains(err.Error(), test.wantMissing) {
				t.Fatalf("expected error to name %q, got %q", test.wantMissing, err)
			}
		})
	}
}

func TestFrontendAssetsFromFSAcceptsCompleteBundle(t *testing.T) {
	files := fstest.MapFS{
		webIndexPath:       {Data: []byte("web-index")},
		zashboardIndexPath: {Data: []byte("zashboard-index")},
		webBuildDir + "/app.js": {
			Data: []byte("app"),
		},
	}

	assets, err := frontendAssetsFromFS(files)
	if err != nil {
		t.Fatalf("load complete asset bundle: %v", err)
	}
	if got := string(assets.indexPage); got != "web-index" {
		t.Fatalf("unexpected WebUI entrypoint: %q", got)
	}

	if _, err := fs.Stat(assets.fs, webBuildDir+"/app.js"); err != nil {
		t.Fatalf("asset filesystem was not preserved: %v", err)
	}
}
