package main

import (
	"os"

	"github.com/jkroepke/azure-monitor-exporter/pkg/cmd/exporter"
)

func main() {
	os.Exit(exporter.Run())
}
