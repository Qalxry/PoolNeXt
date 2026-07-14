//go:build !embedassets

package main

import "os"

// loadFrontendAssets keeps ordinary Go builds and tests independent from
// generated frontend output. Development binaries load assets from the
// server working directory and fail clearly at startup if they are absent.
func loadFrontendAssets() (frontendAssets, error) {
	return frontendAssetsFromFS(os.DirFS("."))
}
