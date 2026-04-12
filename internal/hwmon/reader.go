// Package hwmon reads hardware sensor data from /sys/class/hwmon.
package hwmon

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moeilijk/lhm-companion/internal/server"
)

const sysfsBase = "/sys/class/hwmon"

// Snapshot holds min/max tracking per reading across the process lifetime.
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

// ReadAll reads all hwmon devices and returns a tree of sensor nodes.
func ReadAll() []server.Node {
	entries, err := os.ReadDir(sysfsBase)
	if err != nil {
		return nil
	}

	var nodes []server.Node
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "hwmon") {
			continue
		}
		dir := filepath.Join(sysfsBase, e.Name())
		n := readDevice(dir, e.Name())
		if n != nil {
			nodes = append(nodes, *n)
		}
	}
	return nodes
}

func readDevice(dir, hwmonName string) *server.Node {
	name := readFile(filepath.Join(dir, "name"))
	if name == "" {
		name = hwmonName
	}

	groups := map[string][]server.Node{}

	addReadings(dir, hwmonName, "temp", "Temperature", "°C", 1e-3, groups)
	addReadings(dir, hwmonName, "fan", "Fan", "RPM", 1, groups)
	addReadings(dir, hwmonName, "in", "Voltage", "V", 1e-3, groups)
	addReadings(dir, hwmonName, "power", "Power", "W", 1e-6, groups)
	addReadings(dir, hwmonName, "curr", "Current", "A", 1e-3, groups)
	addFreqReadings(dir, hwmonName, groups)

	if len(groups) == 0 {
		return nil
	}

	groupOrder := []string{"Temperature", "Fan", "Voltage", "Power", "Current", "Clock"}
	var children []server.Node
	for _, g := range groupOrder {
		if readings, ok := groups[g]; ok {
			children = append(children, server.Node{
				Text:     g + "s",
				Children: readings,
			})
		}
	}

	return &server.Node{
		Text:     name,
		Children: children,
	}
}

func addReadings(dir, hwmonName, prefix, typeName, unit string, scale float64, groups map[string][]server.Node) {
	files, _ := filepath.Glob(filepath.Join(dir, prefix+"*_input"))
	sort.Strings(files)

	for _, f := range files {
		idx := extractIndex(filepath.Base(f), prefix, "_input")
		raw := readFileInt(f)
		if raw == math.MinInt64 {
			continue
		}
		val := float64(raw) * scale

		label := readFile(filepath.Join(dir, prefix+idx+"_label"))
		if label == "" {
			label = typeName + " " + idx
		}

		minRaw := readFileInt(filepath.Join(dir, prefix+idx+"_min"))
		maxRaw := readFileInt(filepath.Join(dir, prefix+idx+"_max"))
		if maxRaw == math.MinInt64 {
			maxRaw = readFileInt(filepath.Join(dir, prefix+idx+"_crit"))
		}

		sensorId := fmt.Sprintf("/%s/%s/%s", hwmonName, strings.ToLower(typeName), idx)
		tMin, tMax := track(sensorId, val)

		var minStr, maxStr string
		if minRaw != math.MinInt64 {
			minStr = formatVal(float64(minRaw)*scale, unit)
		} else {
			minStr = formatVal(tMin, unit)
		}
		if maxRaw != math.MinInt64 {
			maxStr = formatVal(float64(maxRaw)*scale, unit)
		} else {
			maxStr = formatVal(tMax, unit)
		}

		// SensorId leaf format: /<hwmon>/<type>/<idx>/0
		// Service.sensorIDFromReading strips last 2 parts → /<hwmon>/<type>/<idx>
		leafId := sensorId + "/0"

		groups[typeName] = append(groups[typeName], server.Node{
			Text:     label,
			Value:    formatVal(val, unit),
			Min:      minStr,
			Max:      maxStr,
			SensorId: leafId,
			Type:     typeName,
		})
	}
}

func addFreqReadings(dir, hwmonName string, groups map[string][]server.Node) {
	files, _ := filepath.Glob(filepath.Join(dir, "freq*_input"))
	sort.Strings(files)

	for _, f := range files {
		idx := extractIndex(filepath.Base(f), "freq", "_input")
		raw := readFileInt(f)
		if raw == math.MinInt64 {
			continue
		}
		val := float64(raw) / 1e6 // Hz → MHz

		label := readFile(filepath.Join(dir, "freq"+idx+"_label"))
		if label == "" {
			label = "Clock " + idx
		}

		sensorId := fmt.Sprintf("/%s/clock/%s", hwmonName, idx)
		tMin, tMax := track(sensorId, val)

		groups["Clock"] = append(groups["Clock"], server.Node{
			Text:     label,
			Value:    formatVal(val, "MHz"),
			Min:      formatVal(tMin, "MHz"),
			Max:      formatVal(tMax, "MHz"),
			SensorId: sensorId + "/0",
			Type:     "Clock",
		})
	}
}

func formatVal(v float64, unit string) string {
	if unit == "" {
		return fmt.Sprintf("%.1f", v)
	}
	// Match LHM decimal comma format
	s := strconv.FormatFloat(v, 'f', 1, 64)
	s = strings.ReplaceAll(s, ".", ",")
	return s + " " + unit
}

func extractIndex(filename, prefix, suffix string) string {
	s := strings.TrimPrefix(filename, prefix)
	s = strings.TrimSuffix(s, suffix)
	return s
}

func readFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func readFileInt(path string) int64 {
	s := readFile(path)
	if s == "" {
		return math.MinInt64
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return math.MinInt64
	}
	return v
}

// Hostname returns the local hostname for the root node label.
func Hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "Linux"
	}
	return h
}

// PollTime returns a nanosecond timestamp usable as the LHM poll time.
func PollTime() uint64 {
	return uint64(time.Now().UnixNano())
}
