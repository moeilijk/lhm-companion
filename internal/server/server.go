package server

import (
	"encoding/json"
	"log"
	"net/http"
)

// Provider builds the sensor tree on demand.
type Provider func() Node

// New returns an http.Handler that serves /data.json using the given provider.
func New(provide Provider) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/data.json", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/data.json", func(w http.ResponseWriter, r *http.Request) {
		root := provide()
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(root); err != nil {
			log.Printf("encode: %v", err)
		}
	})
	return mux
}
