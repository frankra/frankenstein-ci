package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
)

type InfoResponse struct {
	Message string `json:"message"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	UID     int    `json:"uid"`
	GID     int    `json:"gid"`
	PID     int    `json:"pid"`
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(InfoResponse{
			Message: "Hello from a distroless Wolfi container built with melange + apko!",
			OS:      runtime.GOOS,
			Arch:    runtime.GOARCH,
			UID:     os.Getuid(),
			GID:     os.Getgid(),
			PID:     os.Getpid(),
		})
	})

	addr := ":" + port
	fmt.Printf("Starting server on %s (uid=%d, gid=%d, pid=%d)\n", addr, os.Getuid(), os.Getgid(), os.Getpid())
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
