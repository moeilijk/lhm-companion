// Package nvidia reads GPU data via nvidia-smi.
// If nvidia-smi is not present, ReadAll returns nil without error.
package nvidia

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/moeilijk/lhm-companion/internal/server"
)

const query = "name,temperature.gpu,fan.speed,utilization.gpu,utilization.memory,power.draw,power.limit,clocks.current.graphics,clocks.current.memory"

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

	prefix := fmt.Sprintf("/nvidia/%d", gpuIdx)

	var groups []server.Node

	if t := parseFloat(fields[1]); t > 0 {
		groups = appendGroup(groups, "Temperatures", server.Node{
			Text:     "GPU Core",
			Value:    fmtVal(t, "°C"),
			Min:      fmtVal(t, "°C"),
			Max:      fmtVal(t, "°C"),
			SensorId: prefix + "/temperature/0/0",
			Type:     "Temperature",
		})
	}

	if f := parseFloat(fields[2]); f >= 0 {
		groups = appendGroup(groups, "Fans", server.Node{
			Text:     "GPU Fan",
			Value:    fmtVal(f, "%"),
			Min:      fmtVal(f, "%"),
			Max:      fmtVal(f, "%"),
			SensorId: prefix + "/fan/0/0",
			Type:     "Fan",
		})
	}

	if u := parseFloat(fields[3]); u >= 0 {
		groups = appendGroup(groups, "Load", server.Node{
			Text:     "GPU Core",
			Value:    fmtVal(u, "%"),
			Min:      fmtVal(u, "%"),
			Max:      fmtVal(u, "%"),
			SensorId: prefix + "/load/0/0",
			Type:     "Load",
		})
	}
	if u := parseFloat(fields[4]); u >= 0 {
		groups = appendGroup(groups, "Load", server.Node{
			Text:     "GPU Memory",
			Value:    fmtVal(u, "%"),
			Min:      fmtVal(u, "%"),
			Max:      fmtVal(u, "%"),
			SensorId: prefix + "/load/1/0",
			Type:     "Load",
		})
	}

	if p := parseFloat(fields[5]); p > 0 {
		limit := parseFloat(fields[6])
		maxStr := fmtVal(p, "W")
		if limit > 0 {
			maxStr = fmtVal(limit, "W")
		}
		groups = appendGroup(groups, "Powers", server.Node{
			Text:     "GPU Power",
			Value:    fmtVal(p, "W"),
			Min:      fmtVal(p, "W"),
			Max:      maxStr,
			SensorId: prefix + "/power/0/0",
			Type:     "Power",
		})
	}

	if c := parseFloat(fields[7]); c > 0 {
		groups = appendGroup(groups, "Clocks", server.Node{
			Text:     "GPU Core",
			Value:    fmtVal(c, "MHz"),
			Min:      fmtVal(c, "MHz"),
			Max:      fmtVal(c, "MHz"),
			SensorId: prefix + "/clock/0/0",
			Type:     "Clock",
		})
	}
	if c := parseFloat(fields[8]); c > 0 {
		groups = appendGroup(groups, "Clocks", server.Node{
			Text:     "GPU Memory",
			Value:    fmtVal(c, "MHz"),
			Min:      fmtVal(c, "MHz"),
			Max:      fmtVal(c, "MHz"),
			SensorId: prefix + "/clock/1/0",
			Type:     "Clock",
		})
	}

	if len(groups) == 0 {
		return nil
	}

	return &server.Node{Text: name, Children: groups}
}

func appendGroup(groups []server.Node, groupName string, reading server.Node) []server.Node {
	for i := range groups {
		if groups[i].Text == groupName {
			groups[i].Children = append(groups[i].Children, reading)
			return groups
		}
	}
	return append(groups, server.Node{Text: groupName, Children: []server.Node{reading}})
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
