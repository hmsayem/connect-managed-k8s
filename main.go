package main

import (
	"log"

	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Printf("Warning: .env file not found, using environment variables")
	}

	if err := RunGCPTest(); err != nil {
		log.Fatalf("AWS test failed: %v", err)
	}
}
