package main

import (
	"log"

	"github.com/CharlieAIO/sol-cloud/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
