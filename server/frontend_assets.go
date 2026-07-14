package main

import (
	"fmt"
	"io/fs"
)

const (
	webBuildDir        = "web/build"
	webIndexPath       = webBuildDir + "/index.html"
	zashboardBuildDir  = "zashboard/dist"
	zashboardIndexPath = zashboardBuildDir + "/index.html"
)

type frontendAssets struct {
	fs        fs.FS
	indexPage []byte
}

func frontendAssetsFromFS(assetFS fs.FS) (frontendAssets, error) {
	indexPage, err := fs.ReadFile(assetFS, webIndexPath)
	if err != nil {
		return frontendAssets{}, fmt.Errorf("read WebUI entrypoint %q: %w", webIndexPath, err)
	}

	if _, err := fs.Stat(assetFS, zashboardIndexPath); err != nil {
		return frontendAssets{}, fmt.Errorf("read Zashboard entrypoint %q: %w", zashboardIndexPath, err)
	}

	return frontendAssets{fs: assetFS, indexPage: indexPage}, nil
}
