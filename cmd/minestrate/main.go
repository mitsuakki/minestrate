package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Isolated Minecraft minigame servers, on demand. REST API over Docker, written in Go.")
		return
	}

	switch os.Args[1] {
	case "--version":
		fmt.Println("Version: dev")

	default:
		fmt.Println("unknown command")
	}
}