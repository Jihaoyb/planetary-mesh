package main

import (
	"context"
	"os"

	"planetary-mesh/internal/pmctl"
)

func main() {
	os.Exit(pmctl.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
