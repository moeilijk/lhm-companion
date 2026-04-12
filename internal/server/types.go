package server

// Node mirrors the data.json structure produced by Libre Hardware Monitor.
// Leaf nodes have SensorId set; branch nodes have Children set.
type Node struct {
	Text     string `json:"Text"`
	Value    string `json:"Value"`
	Min      string `json:"Min"`
	Max      string `json:"Max"`
	SensorId string `json:"SensorId,omitempty"`
	Type     string `json:"Type,omitempty"`
	ImageURL string `json:"ImageURL,omitempty"`
	Children []Node `json:"Children"`
}
