package main

import (
	"fmt"
	"os"

	"github.com/openmcp-project/platform-service-gateway/cmd/platform-service-gateway/app"
)

func main() {
	cmd := app.NewPlatformServiceGatewayCommand()

	if err := cmd.Execute(); err != nil {
		fmt.Print(err)
		os.Exit(1)
	}
}
