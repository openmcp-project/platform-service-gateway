package main

import (
	"context"
	"fmt"
	"os"

	"github.com/openmcp-project/controller-utils/pkg/fips"

	"github.com/openmcp-project/platform-service-gateway/cmd/platform-service-gateway/app"
)

func main() {
	fips.Verify(context.Background())

	cmd := app.NewPlatformServiceGatewayCommand()

	if err := cmd.Execute(); err != nil {
		fmt.Print(err)
		os.Exit(1)
	}
}
