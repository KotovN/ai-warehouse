package main

import (
	"os"

	"github.com/KotovN/ai-warehouse/internal/cli"
)

func main() {
	if err := cli.New().Execute(); err != nil {
		os.Exit(1)
	}
}
