package main

import (
	"log"
	_ "poolx/docs"
	"poolx/internal/app"
)

// @title PoolNeXt Server API
// @version 3.0
// @description PoolNeXt Server API documentation.
// @BasePath /
// @schemes http https
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Admin API can use Bearer Token.
func main() {
	assets, err := loadFrontendAssets()
	if err != nil {
		log.Fatalf("load frontend assets: %v", err)
	}

	app.RunServer(assets.fs, webBuildDir, assets.indexPage, zashboardBuildDir)
}
