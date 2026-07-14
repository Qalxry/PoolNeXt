//go:build embedassets

package main

import (
	"io/fs"
	"testing"
)

func TestProductionBuildEmbedsBothFrontends(t *testing.T) {
	assets, err := loadFrontendAssets()
	if err != nil {
		t.Fatalf("load embedded frontend assets: %v", err)
	}

	for _, entrypoint := range []string{webIndexPath, zashboardIndexPath} {
		contents, err := fs.ReadFile(assets.fs, entrypoint)
		if err != nil {
			t.Fatalf("read embedded entrypoint %q: %v", entrypoint, err)
		}
		if len(contents) == 0 {
			t.Fatalf("embedded entrypoint %q is empty", entrypoint)
		}
	}
}
