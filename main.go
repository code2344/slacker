package main

import (
	"log"
	"os"

	"github.com/code2344/slacker/cmd"
)

func main() {
	if err := cmd.Run(); err != nil {
		log.SetOutput(os.Stderr)
		log.Fatal(err)
	}
}
