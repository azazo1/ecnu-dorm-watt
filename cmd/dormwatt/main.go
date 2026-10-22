package main

import (
	"os"

	"github.com/azazo1/ecnu-dorm-watt/internal/cli"
)

func main() {
	os.Exit(cli.Execute(os.Args[1:]))
}
