// Package hwmon reads hardware sensor data from /sys/class/hwmon.
package hwmon

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moeilijk/lhm-companion/internal/server"
)

var sysfsBase = "/sys/class/hwmon"

// Snapshot holds min/max tracking per reading across the process lifetime.
type tracker struct {
	min float64
	max float64
}

var (
	trackMu  sync.Mutex
	tracking = map[string]*tracker{}
)

var (
	pciAddressRE    = regexp.MustCompile(`^[0-9a-fA-F]{4}:[0-9a-fA-F]{2}:[0-9a-fA-F]{2}\.[0-9a-fA-F]$`)
	nvmeNamespaceRE = regexp.MustCompile(`^nvme[0-9]+n[0-9]+(?:p[0-9]+)?$`)
	cpuCoreLabelRE  = regexp.MustCompile(`(?i)^core ([0-9]+)$`)
	cpuPkgLabelRE   = regexp.MustCompile(`(?i)^package id [0-9]+$`)
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
// Names listed in skip are excluded (e.g. "nvidia" when nvidia-smi is used).
func ReadAll(skip ...string) []server.Node {
	entries, err := os.ReadDir(sysfsBase)
	if err != nil {
		return nil
	}

	skipSet := make(map[string]bool, len(skip))
	for _, s := range skip {
		skipSet[s] = true
	}

	var nodes []server.Node
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "hwmon") {
			continue
		}
		dir := filepath.Join(sysfsBase, e.Name())
		n := readDevice(dir, e.Name(), skipSet)
		if n != nil {
			nodes = append(nodes, *n)
		}
	}
	return nodes
}

// groupImageURL maps sensor group names to their LHM icon paths.
var groupImageURL = map[string]string{
	"Temperatures": "images_icon/temperature.png",
	"Fans":         "images_icon/fan.png",
	"Voltages":     "images_icon/voltage.png",
	"Powers":       "images_icon/power.png",
	"Currents":     "images_icon/power.png",
	"Clocks":       "images_icon/clock.png",
}

func readDevice(dir, hwmonName string, skip map[string]bool) *server.Node {
	name := readFile(filepath.Join(dir, "name"))
	if name == "" {
		name = hwmonName
	}
	if skip[name] {
		return nil
	}

	groups := map[string][]server.Node{}
	baseID := deviceBaseID(dir, hwmonName, name)

	addReadings(dir, baseID, "temp", "Temperature", "°C", 1e-3, groups)
	addReadings(dir, baseID, "fan", "Fan", "RPM", 1, groups)
	addReadings(dir, baseID, "in", "Voltage", "V", 1e-3, groups)
	addReadings(dir, baseID, "power", "Power", "W", 1e-6, groups)
	addReadings(dir, baseID, "curr", "Current", "A", 1e-3, groups)
	addFreqReadings(dir, baseID, groups)

	if len(groups) == 0 {
		return nil
	}

	groupOrder := []string{"Temperature", "Fan", "Voltage", "Power", "Current", "Clock"}
	var children []server.Node
	for _, g := range groupOrder {
		plural := g + "s"
		if readings, ok := groups[g]; ok {
			children = append(children, server.Node{
				Text:     plural,
				ImageURL: groupImageURL[plural],
				Children: readings,
			})
		}
	}

	return &server.Node{
		Text:     name,
		ImageURL: "images_icon/chip.png",
		Children: children,
	}
}

func addReadings(dir, baseID, prefix, typeName, unit string, scale float64, groups map[string][]server.Node) {
	files, _ := filepath.Glob(filepath.Join(dir, prefix+"*_input"))
	sortFilesNumeric(files, prefix, "_input")

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
		label = normalizeCPUHWMonLabel(baseID, typeName, label)

		minRaw := readFileInt(filepath.Join(dir, prefix+idx+"_min"))
		maxRaw := readFileInt(filepath.Join(dir, prefix+idx+"_max"))
		if maxRaw == math.MinInt64 {
			maxRaw = readFileInt(filepath.Join(dir, prefix+idx+"_crit"))
		}

		sensorId := fmt.Sprintf("%s/%s/%s", baseID, strings.ToLower(typeName), idx)
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

		valStr := formatVal(val, unit)
		groups[typeName] = append(groups[typeName], server.Node{
			Text:     label,
			Value:    valStr,
			Min:      minStr,
			Max:      maxStr,
			SensorId: sensorId,
			Type:     typeName,
			RawValue: valStr,
			RawMin:   minStr,
			RawMax:   maxStr,
			ImageURL: "images/transparent.png",
		})
	}
}

func normalizeCPUHWMonLabel(baseID, typeName, label string) string {
	if baseID != "/cpu" || typeName != "Temperature" {
		return label
	}
	if matches := cpuCoreLabelRE.FindStringSubmatch(label); matches != nil {
		core, err := strconv.Atoi(matches[1])
		if err == nil {
			return fmt.Sprintf("CPU Core #%d", core+1)
		}
	}
	if cpuPkgLabelRE.MatchString(label) {
		return "CPU Package"
	}
	return label
}

func addFreqReadings(dir, baseID string, groups map[string][]server.Node) {
	files, _ := filepath.Glob(filepath.Join(dir, "freq*_input"))
	sortFilesNumeric(files, "freq", "_input")

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

		sensorId := fmt.Sprintf("%s/clock/%s", baseID, idx)
		tMin, tMax := track(sensorId, val)

		valStr := formatVal(val, "MHz")
		minStr := formatVal(tMin, "MHz")
		maxStr := formatVal(tMax, "MHz")
		groups["Clock"] = append(groups["Clock"], server.Node{
			Text:     label,
			Value:    valStr,
			Min:      minStr,
			Max:      maxStr,
			SensorId: sensorId,
			Type:     "Clock",
			RawValue: valStr,
			RawMin:   minStr,
			RawMax:   maxStr,
			ImageURL: "images/transparent.png",
		})
	}
}

// sortFilesNumeric sorts file paths by the integer embedded between prefix and
// suffix in the base name (e.g. "temp10_input" → 10), avoiding the
// lexicographic mis-ordering of sort.Strings ("10" < "2").
func sortFilesNumeric(files []string, prefix, suffix string) {
	sort.Slice(files, func(i, j int) bool {
		ni := indexFromBase(filepath.Base(files[i]), prefix, suffix)
		nj := indexFromBase(filepath.Base(files[j]), prefix, suffix)
		return ni < nj
	})
}

func indexFromBase(base, prefix, suffix string) int {
	s := strings.TrimPrefix(base, prefix)
	s = strings.TrimSuffix(s, suffix)
	n, _ := strconv.Atoi(s)
	return n
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

func deviceBaseID(dir, hwmonName, name string) string {
	canonical := canonicalDevicePath(dir)
	deviceName := sanitizeIDPart(name)
	if deviceName == "" {
		deviceName = sanitizeIDPart(hwmonName)
	}

	switch {
	case isCPUDevice(name, canonical):
		return "/cpu"
	case isGPUDevice(name, canonical):
		token := firstPathSegment(canonical, isPCIAddress)
		if token == "" {
			token = firstNonEmpty(deviceName, hwmonName)
		}
		return "/gpu-amd/" + sanitizeIDPart(token)
	case isStorageDevice(name, canonical):
		token := storageToken(canonical)
		if token == "" {
			token = firstNonEmpty(deviceName, hwmonName)
		}
		return "/storage/" + sanitizeIDPart(token)
	case isNetworkDevice(canonical):
		token := segmentAfter(canonical, "net")
		if token == "" {
			token = firstNonEmpty(deviceName, hwmonName)
		}
		return "/network/" + sanitizeIDPart(token)
	case isLPCDevice(name, canonical):
		token := platformToken(canonical)
		if token == "" {
			token = firstNonEmpty(deviceName, hwmonName)
		}
		return "/lpc/" + sanitizeIDPart(token)
	case isThermalDevice(name, canonical):
		token := firstNonEmpty(thermalToken(canonical), deviceName, hwmonName)
		return "/thermal/" + sanitizeIDPart(token)
	default:
		token := firstNonEmpty(deviceToken(canonical), deviceName, hwmonName)
		return "/" + sanitizeIDPart(token)
	}
}

func canonicalDevicePath(dir string) string {
	for _, candidate := range []string{filepath.Join(dir, "device"), dir} {
		if resolved, err := filepath.EvalSymlinks(candidate); err == nil {
			return filepath.Clean(resolved)
		}
	}
	return filepath.Clean(dir)
}

func isCPUDevice(name, canonical string) bool {
	switch name {
	case "coretemp", "k10temp", "zenpower", "cpu_thermal", "fam15h_power":
		return true
	}
	return strings.Contains(canonical, "/cpu") || strings.Contains(canonical, "coretemp.") || strings.Contains(canonical, "k10temp.")
}

func isGPUDevice(name, canonical string) bool {
	return name == "amdgpu" || strings.Contains(canonical, "/drm/") || strings.Contains(canonical, "/gpu")
}

func isStorageDevice(name, canonical string) bool {
	return name == "nvme" || name == "drivetemp" || strings.Contains(canonical, "/block/") || strings.Contains(canonical, "/nvme/")
}

func isNetworkDevice(canonical string) bool {
	return strings.Contains(canonical, "/net/")
}

func isLPCDevice(name, canonical string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(canonical, "/platform/") ||
		strings.Contains(canonical, "/isa") ||
		strings.HasPrefix(lower, "nct") ||
		strings.HasPrefix(lower, "it8") ||
		strings.HasPrefix(lower, "w836") ||
		strings.HasPrefix(lower, "f718")
}

func isThermalDevice(name, canonical string) bool {
	lower := strings.ToLower(name)
	return lower == "acpitz" || strings.Contains(canonical, "thermal")
}

func storageToken(canonical string) string {
	segs := pathSegments(canonical)
	for _, seg := range segs {
		if nvmeNamespaceRE.MatchString(seg) {
			return seg
		}
	}
	for _, seg := range segs {
		switch {
		case strings.HasPrefix(seg, "nvme") && len(seg) > len("nvme"):
			// Fall back to the controller name if the namespace path is unavailable.
			return seg
		case strings.HasPrefix(seg, "sd"),
			strings.HasPrefix(seg, "vd"),
			strings.HasPrefix(seg, "xvd"),
			strings.HasPrefix(seg, "hd"),
			strings.HasPrefix(seg, "md"):
			return seg
		}
	}
	return segmentAfter(canonical, "block")
}

func platformToken(canonical string) string {
	return segmentAfter(canonical, "platform")
}

func thermalToken(canonical string) string {
	for _, seg := range pathSegments(canonical) {
		if strings.Contains(strings.ToLower(seg), "thermal") || strings.Contains(strings.ToLower(seg), "tz") {
			return seg
		}
	}
	return ""
}

func deviceToken(canonical string) string {
	segs := pathSegments(canonical)
	if len(segs) == 0 {
		return ""
	}
	return segs[len(segs)-1]
}

func segmentAfter(path, marker string) string {
	segs := pathSegments(path)
	for i := 0; i < len(segs)-1; i++ {
		if segs[i] == marker {
			return segs[i+1]
		}
	}
	return ""
}

func firstPathSegment(path string, match func(string) bool) string {
	for _, seg := range pathSegments(path) {
		if match(seg) {
			return seg
		}
	}
	return ""
}

func pathSegments(path string) []string {
	clean := filepath.Clean(path)
	parts := strings.Split(clean, string(filepath.Separator))
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" && part != "." && part != "/" {
			out = append(out, part)
		}
	}
	return out
}

func isPCIAddress(seg string) bool {
	return pciAddressRE.MatchString(seg)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return "device"
}

func sanitizeIDPart(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '-', r == '_':
			b.WriteRune(r)
		case r == ':':
			b.WriteRune('_')
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	out = strings.ReplaceAll(out, "--", "-")
	if out == "" {
		return "device"
	}
	return out
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
