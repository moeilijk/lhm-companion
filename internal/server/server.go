package server

import (
	"encoding/json"
	"log"
	"net/http"
)

// Provider builds the sensor tree on demand.
type Provider func() Node

// Resetter forgets the recorded minimum and maximum of the sensor with the
// given SensorId and reports whether that sensor is known.
type Resetter func(sensorID string) bool

// New returns an http.Handler that serves /data.json using the given provider
// and handles /Sensor requests with the given resetter.
func New(provide Provider, reset Resetter) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/Sensor", func(w http.ResponseWriter, r *http.Request) {
		handleSensor(w, r, reset)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/data.json", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/data.json", func(w http.ResponseWriter, r *http.Request) {
		computer := provide()
		root := Node{
			Text:  "Sensor",
			Min:   "Min",
			Value: "Value",
			Max:   "Max",
			Children: []Node{computer},
		}
		counter := 0
		assignIDs(&root, &counter)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(root); err != nil {
			log.Printf("encode: %v", err)
		}
	})
	return mux
}

func assignIDs(n *Node, counter *int) {
	n.ID = *counter
	*counter++
	for i := range n.Children {
		assignIDs(&n.Children[i], counter)
	}
}

// handleSensor mirrors the /Sensor endpoint of Libre Hardware Monitor for the
// ResetMinMax action:
//
//	POST /Sensor?action=ResetMinMax&id=/some/sensor/path
//
// The reply is {"result":"ok"} or {"result":"fail","message":"..."}.
func handleSensor(w http.ResponseWriter, r *http.Request, reset Resetter) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	query := r.URL.Query()
	action, id := query.Get("action"), query.Get("id")
	result := map[string]string{"result": "ok"}
	fail := func(message string) {
		result["result"] = "fail"
		result["message"] = message
	}
	switch {
	case action == "":
		fail("No action provided")
	case id == "":
		fail("No id provided")
	case action != "ResetMinMax":
		fail("Unknown action type " + action)
	case reset == nil || !reset(id):
		fail("Unknown id " + id + " specified")
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		log.Printf("encode: %v", err)
	}
}
