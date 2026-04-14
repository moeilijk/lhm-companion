package server

import "encoding/json"

// Node mirrors the data.json structure produced by Libre Hardware Monitor.
// Leaf nodes have SensorId set; branch nodes have Children set.
type Node struct {
	ID       int    `json:"id"`
	Text     string `json:"Text"`
	Value    string `json:"Value"`
	Min      string `json:"Min"`
	Max      string `json:"Max"`
	SensorId string `json:"SensorId,omitempty"`
	Type     string `json:"Type,omitempty"`
	RawValue string `json:"RawValue,omitempty"`
	RawMin   string `json:"RawMin,omitempty"`
	RawMax   string `json:"RawMax,omitempty"`
	ImageURL string `json:"ImageURL"`
	Children []Node `json:"Children"`
}

// MarshalJSON ensures Children is always [] instead of null, matching LHM output.
func (n Node) MarshalJSON() ([]byte, error) {
	type plain Node
	children := n.Children
	if children == nil {
		children = []Node{}
	}
	return json.Marshal(plain{
		ID:       n.ID,
		Text:     n.Text,
		Value:    n.Value,
		Min:      n.Min,
		Max:      n.Max,
		SensorId: n.SensorId,
		Type:     n.Type,
		RawValue: n.RawValue,
		RawMin:   n.RawMin,
		RawMax:   n.RawMax,
		ImageURL: n.ImageURL,
		Children: children,
	})
}
