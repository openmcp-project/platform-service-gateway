package main

import (
	"context"
	"crypto/fips140"
	"fmt"
	"os"
	"strings"

	"github.com/openmcp-project/controller-utils/pkg/fips"
	"github.com/openmcp-project/controller-utils/pkg/logging"

	"github.com/openmcp-project/platform-service-gateway/cmd/platform-service-gateway/app"
)

func main() {
	fips.Verify(context.Background())
	Verify2(context.Background())

	cmd := app.NewPlatformServiceGatewayCommand()

	if err := cmd.Execute(); err != nil {
		fmt.Print(err)
		os.Exit(1)
	}
}

func Verify2(ctx context.Context) {
	log, _ := logging.FromContextOrNew(ctx, nil)

	log.Info("verifying FIPS 140-3 compliance")

	fips140V := fips140Value()

	log.Info("FIPS env configuration", "fips140", fips140V)

	if !fips140.Enabled() {
		err := fmt.Errorf("FIPS 140 is disabled but enforcement is enabled")
		log.Error(err, "FIPS 140 is disabled but enforcement is active")
		os.Exit(1)
	}

	log.Info("FIPS 140 is enabled")
}

func fips140Value() string {
	for _, kv := range strings.Split(os.Getenv("GODEBUG"), ",") {
		if strings.HasPrefix(kv, "fips140=") {
			return strings.TrimPrefix(kv, "fips140=")
		}
	}
	return ""
}
