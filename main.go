package main

import (
	"os"

	"github.com/rsevilla/ovnkctl/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
