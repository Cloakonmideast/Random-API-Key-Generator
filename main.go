package main

import (
	"fmt"
	"log"
	"net/http"
)

func main() {
	http.HandleFunc("/generate", handleGenerate)
	http.HandleFunc("/get", handleGetAvailable)
	http.HandleFunc("/unblock", handleUnblock)
	http.HandleFunc("/delete", handleDelete)
	http.HandleFunc("/keepalive", handleKeepAlive)

	fmt.Println("Server starting on :8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
