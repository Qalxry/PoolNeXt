//go:build embedassets

package main

import "embed"

// embeddedFrontendAssets is included only in explicit production builds.
// The build fails at compile time unless both generated frontend trees exist.
//
//go:embed all:web/build all:zashboard/dist
var embeddedFrontendAssets embed.FS

func loadFrontendAssets() (frontendAssets, error) {
	return frontendAssetsFromFS(embeddedFrontendAssets)
}
