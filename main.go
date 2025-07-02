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

	if err := RunAKSTest(); err != nil {
		log.Fatalf("test failed: %v", err)
	}

	if err := RunGKETest(); err != nil {
		log.Fatalf("test failed: %v", err)
	}

	if err := RunEKSTest(); err != nil {
		log.Fatalf("test failed: %v", err)
	}
}
