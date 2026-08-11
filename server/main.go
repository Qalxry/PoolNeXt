package main

import (
	"log"
	_ "poolx/docs"
	"poolx/internal/app"
	"poolx/internal/pkg/common"
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
	if *common.ResetPassword {
		if err := app.ResetAdminPasswordCLI(); err != nil {
			log.Fatalf("reset admin password: %v", err)
		}
		return
	}

	assets, err := loadFrontendAssets()
	if err != nil {
		log.Fatalf("load frontend assets: %v", err)
	}

	app.RunServer(assets.fs, webBuildDir, assets.indexPage, zashboardBuildDir)
}
