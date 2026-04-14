// Package system reads system metrics from /proc and /sys.
package system

import (
	"bufio"
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

var (
	procStatPath = "/proc/stat"
	procMeminfo  = "/proc/meminfo"
	cpuFreqGlob  = "/sys/devices/system/cpu/cpu[0-9]*/cpufreq/scaling_cur_freq"
	netClassPath = "/sys/class/net"
	blockClass   = "/sys/class/block"
	now          = time.Now
)

type tracker struct {
	min float64
	max float64
}

type cpuTimes struct {
	total uint64
	idle  uint64
}

type netSample struct {
	rx uint64
	tx uint64
	at time.Time
}

type blockSample struct {
	readBytes  uint64
	writeBytes uint64
	ioMillis   uint64
	at         time.Time
}

var (
	trackMu     sync.Mutex
	tracking    = map[string]*tracker{}
	cpuPrevMu   sync.Mutex
	cpuPrev     = map[string]cpuTimes{}
	netPrevMu   sync.Mutex
	netPrev     = map[string]netSample{}
	blockPrevMu sync.Mutex
	blockPrev   = map[string]blockSample{}
)

func init() {
	primeSamples()
}

// ReadAll returns system metrics from /proc and /sys in LHM-style nodes.
func ReadAll() []server.Node {
	var nodes []server.Node
	if n := readCPU(); n != nil {
		nodes = append(nodes, *n)
	}
	if n := readMemory(); n != nil {
		nodes = append(nodes, *n)
	}
	if n := readNetwork(); n != nil {
		nodes = append(nodes, *n)
	}
	if n := readStorage(); n != nil {
		nodes = append(nodes, *n)
	}
	return nodes
}

func readCPU() *server.Node {
	current, err := readCPUTimes(procStatPath)
	if err != nil || len(current) == 0 {
		return nil
	}

	loadNodes := buildCPULoadNodes(current)
	clockNodes := readCPUClockNodes()
	if len(loadNodes) == 0 && len(clockNodes) == 0 {
		return nil
	}

	var children []server.Node
	if len(loadNodes) > 0 {
		children = append(children, server.Node{
			Text:     "Load",
			Children: loadNodes,
		})
	}
	if len(clockNodes) > 0 {
		children = append(children, server.Node{
			Text:     "Clocks",
			Children: clockNodes,
		})
	}

	return &server.Node{
		Text:     "CPU",
		Children: children,
	}
}

func readMemory() *server.Node {
	mem, err := readMeminfo(procMeminfo)
	if err != nil {
		return nil
	}

	total := mem["MemTotal"] * 1024
	available := mem["MemAvailable"] * 1024
	if total == 0 {
		return nil
	}
	used := total - minUint64(total, available)

	loadPct := percent(float64(used), float64(total))
	loadMin, loadMax := trackValue("/memory/load/0", loadPct)

	loadNodes := []server.Node{{
		Text:     "Memory",
		Value:    formatValue(loadPct, "%"),
		Min:      formatValue(loadMin, "%"),
		Max:      formatValue(loadMax, "%"),
		SensorId: "/memory/load/0/0",
		Type:     "Load",
	}}

	dataNodes := []server.Node{
		dataNode("/memory/data/used", "Used Memory", float64(used), "/memory/data/0/0"),
		dataNode("/memory/data/available", "Available Memory", float64(available), "/memory/data/1/0"),
		dataNode("/memory/data/total", "Total Memory", float64(total), "/memory/data/2/0"),
	}

	swapTotal := mem["SwapTotal"] * 1024
	swapFree := mem["SwapFree"] * 1024
	if swapTotal > 0 {
		swapUsed := swapTotal - minUint64(swapTotal, swapFree)
		swapLoad := percent(float64(swapUsed), float64(swapTotal))
		swapMin, swapMax := trackValue("/memory/load/swap", swapLoad)
		loadNodes = append(loadNodes, server.Node{
			Text:     "Swap",
			Value:    formatValue(swapLoad, "%"),
			Min:      formatValue(swapMin, "%"),
			Max:      formatValue(swapMax, "%"),
			SensorId: "/memory/load/1/0",
			Type:     "Load",
		})
		dataNodes = append(dataNodes,
			dataNode("/memory/data/swap-used", "Used Swap", float64(swapUsed), "/memory/data/3/0"),
			dataNode("/memory/data/swap-total", "Total Swap", float64(swapTotal), "/memory/data/4/0"),
		)
	}

	return &server.Node{
		Text: "Memory",
		Children: []server.Node{
			{Text: "Load", Children: loadNodes},
			{Text: "Data", Children: dataNodes},
		},
	}
}

func readNetwork() *server.Node {
	entries, err := os.ReadDir(netClassPath)
	if err != nil {
		return nil
	}

	currentTime := now()
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	var ifaces []server.Node
	for _, entry := range entries {
		name := entry.Name()
		if name == "lo" {
			continue
		}

		dir := filepath.Join(netClassPath, name)
		rx := readUint64(filepath.Join(dir, "statistics", "rx_bytes"))
		tx := readUint64(filepath.Join(dir, "statistics", "tx_bytes"))
		if rx == invalidUint64 || tx == invalidUint64 {
			continue
		}

		rxRate, txRate := networkRates(name, rx, tx, currentTime)
		var children []server.Node

		throughputNodes := []server.Node{
			throughputNode("/network/"+name+"/throughput/rx", "Receive", rxRate, "/network/"+name+"/throughput/0/0"),
			throughputNode("/network/"+name+"/throughput/tx", "Transmit", txRate, "/network/"+name+"/throughput/1/0"),
		}
		children = append(children, server.Node{Text: "Throughput", Children: throughputNodes})

		dataNodes := []server.Node{
			dataNode("/network/"+name+"/data/rx", "Received Total", float64(rx), "/network/"+name+"/data/0/0"),
			dataNode("/network/"+name+"/data/tx", "Transmitted Total", float64(tx), "/network/"+name+"/data/1/0"),
		}
		children = append(children, server.Node{Text: "Data", Children: dataNodes})

		if speed := readUint64(filepath.Join(dir, "speed")); speed != invalidUint64 && speed > 0 {
			rxLoad := percent(rxRate*8, float64(speed)*1e6)
			txLoad := percent(txRate*8, float64(speed)*1e6)
			children = append(children, server.Node{
				Text: "Load",
				Children: []server.Node{
					loadNode("/network/"+name+"/load/rx", "Receive", rxLoad, "/network/"+name+"/load/0/0"),
					loadNode("/network/"+name+"/load/tx", "Transmit", txLoad, "/network/"+name+"/load/1/0"),
				},
			})
		}

		ifaces = append(ifaces, server.Node{
			Text:     name,
			Children: children,
		})
	}

	if len(ifaces) == 0 {
		return nil
	}

	return &server.Node{
		Text:     "Network",
		Children: ifaces,
	}
}

func readStorage() *server.Node {
	entries, err := os.ReadDir(blockClass)
	if err != nil {
		return nil
	}

	currentTime := now()
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	var devices []server.Node
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") || strings.HasPrefix(name, "zram") {
			continue
		}

		dir := filepath.Join(blockClass, name)
		if _, err := os.Stat(filepath.Join(dir, "partition")); err == nil {
			continue
		}

		readBytes, writeBytes, ioMillis, ok := readBlockStats(dir)
		if !ok {
			continue
		}

		readRate, writeRate, busy := storageRates(name, readBytes, writeBytes, ioMillis, currentTime)
		label := readFile(filepath.Join(dir, "device", "model"))
		if label == "" {
			label = name
		} else {
			label = strings.TrimSpace(label) + " (" + name + ")"
		}

		children := []server.Node{
			{
				Text: "Throughput",
				Children: []server.Node{
					throughputNode("/storage/"+name+"/throughput/read", "Read", readRate, "/storage/"+name+"/throughput/0/0"),
					throughputNode("/storage/"+name+"/throughput/write", "Write", writeRate, "/storage/"+name+"/throughput/1/0"),
				},
			},
			{
				Text: "Load",
				Children: []server.Node{
					loadNode("/storage/"+name+"/load/activity", "Activity", busy, "/storage/"+name+"/load/0/0"),
				},
			},
			{
				Text: "Data",
				Children: []server.Node{
					dataNode("/storage/"+name+"/data/read", "Read Total", float64(readBytes), "/storage/"+name+"/data/0/0"),
					dataNode("/storage/"+name+"/data/write", "Write Total", float64(writeBytes), "/storage/"+name+"/data/1/0"),
				},
			},
		}

		devices = append(devices, server.Node{
			Text:     label,
			Children: children,
		})
	}

	if len(devices) == 0 {
		return nil
	}

	return &server.Node{
		Text:     "Storage",
		Children: devices,
	}
}

func readCPUTimes(path string) (map[string]cpuTimes, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	stats := map[string]cpuTimes{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 5 || !strings.HasPrefix(fields[0], "cpu") {
			continue
		}

		var total uint64
		var values []uint64
		for _, field := range fields[1:] {
			v, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				return nil, err
			}
			values = append(values, v)
			total += v
		}

		idle := values[3]
		if len(values) > 4 {
			idle += values[4]
		}
		stats[fields[0]] = cpuTimes{total: total, idle: idle}
	}

	return stats, scanner.Err()
}

func buildCPULoadNodes(current map[string]cpuTimes) []server.Node {
	cpuPrevMu.Lock()
	prev := cpuPrev
	cpuPrev = current
	cpuPrevMu.Unlock()

	keys := make([]string, 0, len(current))
	for key := range current {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i] == "cpu" {
			return true
		}
		if keys[j] == "cpu" {
			return false
		}
		return cpuIndex(keys[i]) < cpuIndex(keys[j])
	})

	nodes := make([]server.Node, 0, len(keys))
	for idx, key := range keys {
		cur := current[key]
		usage := 0.0
		if old, ok := prev[key]; ok && cur.total > old.total {
			deltaTotal := cur.total - old.total
			deltaIdle := cur.idle - old.idle
			usage = percent(float64(deltaTotal-deltaIdle), float64(deltaTotal))
		}

		label := "Total"
		idKey := "total"
		if key != "cpu" {
			core := strings.TrimPrefix(key, "cpu")
			label = "Core " + core
			idKey = "core" + core
		}
		min, max := trackValue("/cpu/load/"+idKey, usage)
		nodes = append(nodes, server.Node{
			Text:     label,
			Value:    formatValue(usage, "%"),
			Min:      formatValue(min, "%"),
			Max:      formatValue(max, "%"),
			SensorId: fmt.Sprintf("/cpu/load/%d/0", idx),
			Type:     "Load",
		})
	}
	return nodes
}

func readCPUClockNodes() []server.Node {
	files, _ := filepath.Glob(cpuFreqGlob)
	sort.Strings(files)

	nodes := make([]server.Node, 0, len(files))
	for idx, file := range files {
		raw := readUint64(file)
		if raw == invalidUint64 {
			continue
		}

		core := cpuIndex(filepath.Dir(filepath.Dir(file)))
		value := float64(raw) / 1000.0
		min, max := trackValue(fmt.Sprintf("/cpu/clock/core%d", core), value)
		nodes = append(nodes, server.Node{
			Text:     fmt.Sprintf("Core %d", core),
			Value:    formatValue(value, "MHz"),
			Min:      formatValue(min, "MHz"),
			Max:      formatValue(max, "MHz"),
			SensorId: fmt.Sprintf("/cpu/clock/%d/0", idx),
			Type:     "Clock",
		})
	}
	return nodes
}

func readMeminfo(path string) (map[string]uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	values := map[string]uint64{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		v, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		values[key] = v
	}
	return values, scanner.Err()
}

func networkRates(name string, rx, tx uint64, currentTime time.Time) (float64, float64) {
	netPrevMu.Lock()
	defer netPrevMu.Unlock()

	prev, ok := netPrev[name]
	netPrev[name] = netSample{rx: rx, tx: tx, at: currentTime}
	if !ok {
		return 0, 0
	}

	delta := currentTime.Sub(prev.at).Seconds()
	if delta <= 0 {
		return 0, 0
	}
	return float64(rx-prev.rx) / delta, float64(tx-prev.tx) / delta
}

func readBlockStats(dir string) (uint64, uint64, uint64, bool) {
	fields := strings.Fields(readFile(filepath.Join(dir, "stat")))
	if len(fields) < 10 {
		return 0, 0, 0, false
	}

	readSectors, err1 := strconv.ParseUint(fields[2], 10, 64)
	writeSectors, err2 := strconv.ParseUint(fields[6], 10, 64)
	ioMillis, err3 := strconv.ParseUint(fields[9], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, 0, 0, false
	}

	sectorSize := readUint64(filepath.Join(dir, "queue", "logical_block_size"))
	if sectorSize == invalidUint64 || sectorSize == 0 {
		sectorSize = 512
	}

	return readSectors * sectorSize, writeSectors * sectorSize, ioMillis, true
}

func storageRates(name string, readBytes, writeBytes, ioMillis uint64, currentTime time.Time) (float64, float64, float64) {
	blockPrevMu.Lock()
	defer blockPrevMu.Unlock()

	prev, ok := blockPrev[name]
	blockPrev[name] = blockSample{
		readBytes:  readBytes,
		writeBytes: writeBytes,
		ioMillis:   ioMillis,
		at:         currentTime,
	}
	if !ok {
		return 0, 0, 0
	}

	delta := currentTime.Sub(prev.at).Seconds()
	if delta <= 0 {
		return 0, 0, 0
	}

	readRate := float64(readBytes-prev.readBytes) / delta
	writeRate := float64(writeBytes-prev.writeBytes) / delta
	busy := percent(float64(ioMillis-prev.ioMillis), delta*1000)
	return readRate, writeRate, busy
}

func loadNode(id, label string, value float64, sensorID string) server.Node {
	min, max := trackValue(id, value)
	return server.Node{
		Text:     label,
		Value:    formatValue(value, "%"),
		Min:      formatValue(min, "%"),
		Max:      formatValue(max, "%"),
		SensorId: sensorID,
		Type:     "Load",
	}
}

func throughputNode(id, label string, value float64, sensorID string) server.Node {
	min, max := trackValue(id, value)
	return server.Node{
		Text:     label,
		Value:    formatBytes(value, true),
		Min:      formatBytes(min, true),
		Max:      formatBytes(max, true),
		SensorId: sensorID,
		Type:     "Throughput",
	}
}

func dataNode(id, label string, value float64, sensorID string) server.Node {
	min, max := trackValue(id, value)
	return server.Node{
		Text:     label,
		Value:    formatBytes(value, false),
		Min:      formatBytes(min, false),
		Max:      formatBytes(max, false),
		SensorId: sensorID,
		Type:     "Data",
	}
}

func trackValue(id string, value float64) (float64, float64) {
	trackMu.Lock()
	defer trackMu.Unlock()

	t, ok := tracking[id]
	if !ok {
		t = &tracker{min: value, max: value}
		tracking[id] = t
	}
	if value < t.min {
		t.min = value
	}
	if value > t.max {
		t.max = value
	}
	return t.min, t.max
}

func formatValue(v float64, unit string) string {
	s := strconv.FormatFloat(v, 'f', 1, 64)
	s = strings.ReplaceAll(s, ".", ",")
	if unit == "" {
		return s
	}
	return s + " " + unit
}

func formatBytes(v float64, perSecond bool) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	idx := 0
	for v >= 1024 && idx < len(units)-1 {
		v /= 1024
		idx++
	}
	unit := units[idx]
	if perSecond {
		unit += "/s"
	}
	return formatValue(v, unit)
}

const invalidUint64 = math.MaxUint64

func readUint64(path string) uint64 {
	s := readFile(path)
	if s == "" {
		return invalidUint64
	}
	v, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return invalidUint64
	}
	return v
}

func readFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func cpuIndex(name string) int {
	base := filepath.Base(name)
	base = strings.TrimPrefix(base, "cpu")
	i, err := strconv.Atoi(base)
	if err != nil {
		return math.MaxInt32
	}
	return i
}

func percent(part, total float64) float64 {
	if total <= 0 {
		return 0
	}
	pct := (part / total) * 100
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

func minUint64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

func primeSamples() {
	currentTime := now()

	if stats, err := readCPUTimes(procStatPath); err == nil {
		cpuPrevMu.Lock()
		cpuPrev = stats
		cpuPrevMu.Unlock()
	}

	if entries, err := os.ReadDir(netClassPath); err == nil {
		samples := map[string]netSample{}
		for _, entry := range entries {
			name := entry.Name()
			if name == "lo" {
				continue
			}
			dir := filepath.Join(netClassPath, name)
			rx := readUint64(filepath.Join(dir, "statistics", "rx_bytes"))
			tx := readUint64(filepath.Join(dir, "statistics", "tx_bytes"))
			if rx == invalidUint64 || tx == invalidUint64 {
				continue
			}
			samples[name] = netSample{rx: rx, tx: tx, at: currentTime}
		}
		netPrevMu.Lock()
		netPrev = samples
		netPrevMu.Unlock()
	}

	if entries, err := os.ReadDir(blockClass); err == nil {
		samples := map[string]blockSample{}
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") || strings.HasPrefix(name, "zram") {
				continue
			}
			dir := filepath.Join(blockClass, name)
			if _, err := os.Stat(filepath.Join(dir, "partition")); err == nil {
				continue
			}
			readBytes, writeBytes, ioMillis, ok := readBlockStats(dir)
			if !ok {
				continue
			}
			samples[name] = blockSample{
				readBytes:  readBytes,
				writeBytes: writeBytes,
				ioMillis:   ioMillis,
				at:         currentTime,
			}
		}
		blockPrevMu.Lock()
		blockPrev = samples
		blockPrevMu.Unlock()
	}
}
