package main

import (
	"encoding/json"
	"net/http"
)

var server = NewKeyServer()

func handleGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{
		"key": server.GenerateKey(),
	})
}

func handleGetAvailable(w http.ResponseWriter, r *http.Request) {
	key, ok := server.GetAvailableKey()
	if !ok {
		http.Error(w, "No available keys", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"key": key})
}

func handleUnblock(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	server.UnblockKey(id)
	w.Write([]byte("Unblocked"))
}

func handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	server.DeleteKey(id)
	w.Write([]byte("Deleted"))
}

func handleKeepAlive(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if !server.KeepAlive(id) {
		http.Error(w, "Key not found", http.StatusNotFound)
		return
	}
	w.Write([]byte("KeepAlive Received"))
}
