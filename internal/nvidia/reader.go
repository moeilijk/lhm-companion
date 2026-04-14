// Package nvidia reads GPU data via nvidia-smi.
// If nvidia-smi is not present, ReadAll returns nil without error.
package nvidia

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/moeilijk/lhm-companion/internal/server"
)

const query = "name,temperature.gpu,fan.speed,utilization.gpu,utilization.memory,power.draw,power.limit,clocks.current.graphics,clocks.current.memory"

type tracker struct {
	min float64
	max float64
}

var (
	trackMu  sync.Mutex
	tracking = map[string]*tracker{}
)

func track(id string, val float64) (min, max float64) {
	trackMu.Lock()
	defer trackMu.Unlock()
	t, ok := tracking[id]
	if !ok {
		t = &tracker{min: val, max: val}
		tracking[id] = t
	}
	if val < t.min {
		t.min = val
	}
	if val > t.max {
		t.max = val
	}
	return t.min, t.max
}

// Available reports whether nvidia-smi is present on the system.
func Available() bool {
	_, err := exec.LookPath("nvidia-smi")
	return err == nil
}

// ReadAll queries all GPUs and returns sensor nodes.
func ReadAll() []server.Node {
	out, err := exec.Command(
		"nvidia-smi",
		"--query-gpu="+query,
		"--format=csv,noheader,nounits",
	).Output()
	if err != nil {
		return nil
	}

	var nodes []server.Node
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for i, line := range lines {
		n := parseLine(i, strings.TrimSpace(line))
		if n != nil {
			nodes = append(nodes, *n)
		}
	}
	return nodes
}

func parseLine(gpuIdx int, line string) *server.Node {
	fields := strings.Split(line, ", ")
	if len(fields) < 9 {
		return nil
	}

	name := strings.TrimSpace(fields[0])
	if name == "" {
		name = fmt.Sprintf("GPU %d", gpuIdx)
	}

	prefix := fmt.Sprintf("/gpu-nvidia/%d", gpuIdx)

	var groups []server.Node

	if t := parseFloat(fields[1]); t >= 0 {
		tMin, tMax := track(prefix+"/temperature/0", t)
		groups = appendGroup(groups, "Temperatures", "images_icon/temperature.png", sensorNode(
			"GPU Core", fmtVal(t, "°C"), fmtVal(tMin, "°C"), fmtVal(tMax, "°C"),
			prefix+"/temperature/0", "Temperature",
		))
	}

	// fan.speed from nvidia-smi is duty cycle (%) → LHM "Control" type in "Controls" group
	if f := parseFloat(fields[2]); f >= 0 {
		fMin, fMax := track(prefix+"/control/0", f)
		groups = appendGroup(groups, "Controls", "images_icon/control.png", sensorNode(
			"GPU Fan", fmtVal(f, "%"), fmtVal(fMin, "%"), fmtVal(fMax, "%"),
			prefix+"/control/0", "Control",
		))
	}

	if u := parseFloat(fields[3]); u >= 0 {
		uMin, uMax := track(prefix+"/load/0", u)
		groups = appendGroup(groups, "Load", "images_icon/load.png", sensorNode(
			"GPU Core", fmtVal(u, "%"), fmtVal(uMin, "%"), fmtVal(uMax, "%"),
			prefix+"/load/0", "Load",
		))
	}
	if u := parseFloat(fields[4]); u >= 0 {
		uMin, uMax := track(prefix+"/load/1", u)
		groups = appendGroup(groups, "Load", "images_icon/load.png", sensorNode(
			"GPU Memory", fmtVal(u, "%"), fmtVal(uMin, "%"), fmtVal(uMax, "%"),
			prefix+"/load/1", "Load",
		))
	}

	if p := parseFloat(fields[5]); p >= 0 {
		limit := parseFloat(fields[6])
		pMin, pMax := track(prefix+"/power/0", p)
		maxStr := fmtVal(pMax, "W")
		if limit > pMax {
			maxStr = fmtVal(limit, "W")
		}
		groups = appendGroup(groups, "Powers", "images_icon/power.png", sensorNode(
			"GPU Power", fmtVal(p, "W"), fmtVal(pMin, "W"), maxStr,
			prefix+"/power/0", "Power",
		))
	}

	if c := parseFloat(fields[7]); c >= 0 {
		cMin, cMax := track(prefix+"/clock/0", c)
		groups = appendGroup(groups, "Clocks", "images_icon/clock.png", sensorNode(
			"GPU Core", fmtVal(c, "MHz"), fmtVal(cMin, "MHz"), fmtVal(cMax, "MHz"),
			prefix+"/clock/0", "Clock",
		))
	}
	if c := parseFloat(fields[8]); c >= 0 {
		cMin, cMax := track(prefix+"/clock/1", c)
		groups = appendGroup(groups, "Clocks", "images_icon/clock.png", sensorNode(
			"GPU Memory", fmtVal(c, "MHz"), fmtVal(cMin, "MHz"), fmtVal(cMax, "MHz"),
			prefix+"/clock/1", "Clock",
		))
	}

	if len(groups) == 0 {
		return nil
	}

	return &server.Node{
		Text:     name,
		ImageURL: "images_icon/nvidia.png",
		Children: groups,
	}
}

func sensorNode(text, value, min, max, sensorId, sensorType string) server.Node {
	return server.Node{
		Text:     text,
		Value:    value,
		Min:      min,
		Max:      max,
		SensorId: sensorId,
		Type:     sensorType,
		RawValue: value,
		RawMin:   min,
		RawMax:   max,
		ImageURL: "images/transparent.png",
	}
}

func appendGroup(groups []server.Node, groupName, imageURL string, reading server.Node) []server.Node {
	for i := range groups {
		if groups[i].Text == groupName {
			groups[i].Children = append(groups[i].Children, reading)
			return groups
		}
	}
	return append(groups, server.Node{
		Text:     groupName,
		ImageURL: imageURL,
		Children: []server.Node{reading},
	})
}

func parseFloat(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "[N/A]" || s == "N/A" {
		return -1
	}
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func fmtVal(v float64, unit string) string {
	s := strconv.FormatFloat(v, 'f', 1, 64)
	s = strings.ReplaceAll(s, ".", ",")
	if unit != "" {
		return s + " " + unit
	}
	return s
}
