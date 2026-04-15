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
	cpuSysBase   = "/sys/devices/system/cpu"
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

type cpuTopology struct {
	cpu           int
	packageID     int
	coreID        int
	coreOrdinal   int
	threadOrdinal int
	threadCount   int
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
			ImageURL: "images_icon/load.png",
			Children: loadNodes,
		})
	}
	if len(clockNodes) > 0 {
		children = append(children, server.Node{
			Text:     "Clocks",
			ImageURL: "images_icon/clock.png",
			Children: clockNodes,
		})
	}

	return &server.Node{
		Text:     "CPU",
		ImageURL: "images_icon/cpu.png",
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

	loadNodes := []server.Node{
		loadNode("/memory/load/0", "Memory", loadPct, "/memory/load/0"),
	}
	// Override SensorId since loadNode generates it internally
	loadNodes[0].SensorId = "/memory/load/0"
	loadNodes[0].Min = formatValue(loadMin, "%")
	loadNodes[0].Max = formatValue(loadMax, "%")
	loadNodes[0].RawMin = loadNodes[0].Min
	loadNodes[0].RawMax = loadNodes[0].Max

	dataNodes := []server.Node{
		dataNode("/memory/data/0", "Used Memory", float64(used), "/memory/data/0"),
		dataNode("/memory/data/1", "Available Memory", float64(available), "/memory/data/1"),
		dataNode("/memory/data/2", "Total Memory", float64(total), "/memory/data/2"),
	}

	swapTotal := mem["SwapTotal"] * 1024
	swapFree := mem["SwapFree"] * 1024
	if swapTotal > 0 {
		swapUsed := swapTotal - minUint64(swapTotal, swapFree)
		swapLoad := percent(float64(swapUsed), float64(swapTotal))
		swapMin, swapMax := trackValue("/memory/load/swap", swapLoad)
		swapValStr := formatValue(swapLoad, "%")
		loadNodes = append(loadNodes, server.Node{
			Text:     "Swap",
			Value:    swapValStr,
			Min:      formatValue(swapMin, "%"),
			Max:      formatValue(swapMax, "%"),
			SensorId: "/memory/load/1",
			Type:     "Load",
			RawValue: swapValStr,
			RawMin:   formatValue(swapMin, "%"),
			RawMax:   formatValue(swapMax, "%"),
			ImageURL: "images/transparent.png",
		})
		dataNodes = append(dataNodes,
			dataNode("/memory/data/3", "Used Swap", float64(swapUsed), "/memory/data/3"),
			dataNode("/memory/data/4", "Total Swap", float64(swapTotal), "/memory/data/4"),
		)
	}

	return &server.Node{
		Text:     "Memory",
		ImageURL: "images_icon/ram.png",
		Children: []server.Node{
			{Text: "Load", ImageURL: "images_icon/load.png", Children: loadNodes},
			{Text: "Data", ImageURL: "images_icon/power.png", Children: dataNodes},
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
			throughputNode("/network/"+name+"/throughput/rx", "Receive", rxRate, "/network/"+name+"/throughput/0"),
			throughputNode("/network/"+name+"/throughput/tx", "Transmit", txRate, "/network/"+name+"/throughput/1"),
		}
		children = append(children, server.Node{Text: "Throughput", ImageURL: "images_icon/throughput.png", Children: throughputNodes})

		dataNodes := []server.Node{
			dataNode("/network/"+name+"/data/rx", "Received Total", float64(rx), "/network/"+name+"/data/0"),
			dataNode("/network/"+name+"/data/tx", "Transmitted Total", float64(tx), "/network/"+name+"/data/1"),
		}
		children = append(children, server.Node{Text: "Data", ImageURL: "images_icon/power.png", Children: dataNodes})

		if speed := readUint64(filepath.Join(dir, "speed")); speed != invalidUint64 && speed > 0 {
			rxLoad := percent(rxRate*8, float64(speed)*1e6)
			txLoad := percent(txRate*8, float64(speed)*1e6)
			children = append(children, server.Node{
				Text:     "Load",
				ImageURL: "images_icon/load.png",
				Children: []server.Node{
					loadNode("/network/"+name+"/load/rx", "Receive", rxLoad, "/network/"+name+"/load/0"),
					loadNode("/network/"+name+"/load/tx", "Transmit", txLoad, "/network/"+name+"/load/1"),
				},
			})
		}

		ifaces = append(ifaces, server.Node{
			Text:     name,
			ImageURL: "images_icon/nic.png",
			Children: children,
		})
	}

	if len(ifaces) == 0 {
		return nil
	}

	return &server.Node{
		Text:     "Network",
		ImageURL: "images_icon/nic.png",
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
				Text:     "Throughput",
				ImageURL: "images_icon/throughput.png",
				Children: []server.Node{
					throughputNode("/storage/"+name+"/throughput/read", "Read", readRate, "/storage/"+name+"/throughput/0"),
					throughputNode("/storage/"+name+"/throughput/write", "Write", writeRate, "/storage/"+name+"/throughput/1"),
				},
			},
			{
				Text:     "Load",
				ImageURL: "images_icon/load.png",
				Children: []server.Node{
					loadNode("/storage/"+name+"/load/activity", "Activity", busy, "/storage/"+name+"/load/0"),
				},
			},
			{
				Text:     "Data",
				ImageURL: "images_icon/power.png",
				Children: []server.Node{
					dataNode("/storage/"+name+"/data/read", "Read Total", float64(readBytes), "/storage/"+name+"/data/0"),
					dataNode("/storage/"+name+"/data/write", "Write Total", float64(writeBytes), "/storage/"+name+"/data/1"),
				},
			},
		}

		devices = append(devices, server.Node{
			Text:     label,
			ImageURL: "images_icon/hdd.png",
			Children: children,
		})
	}

	if len(devices) == 0 {
		return nil
	}

	return &server.Node{
		Text:     "Storage",
		ImageURL: "images_icon/hdd.png",
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
	topology := readCPUTopology()

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
	for _, key := range keys {
		cur := current[key]
		usage := 0.0
		if old, ok := prev[key]; ok && cur.total > old.total {
			deltaTotal := cur.total - old.total
			deltaIdle := cur.idle - old.idle
			usage = percent(float64(deltaTotal-deltaIdle), float64(deltaTotal))
		}

		label := "CPU Total"
		sensorID := "/cpu/load/total"
		if key != "cpu" {
			label, sensorID = cpuLoadMetadata(cpuIndex(key), topology)
		}
		min, max := trackValue(sensorID, usage)
		valStr := formatValue(usage, "%")
		minStr := formatValue(min, "%")
		maxStr := formatValue(max, "%")
		nodes = append(nodes, server.Node{
			Text:     label,
			Value:    valStr,
			Min:      minStr,
			Max:      maxStr,
			SensorId: sensorID,
			Type:     "Load",
			RawValue: valStr,
			RawMin:   minStr,
			RawMax:   maxStr,
			ImageURL: "images/transparent.png",
		})
	}
	return nodes
}

func readCPUClockNodes() []server.Node {
	files, _ := filepath.Glob(cpuFreqGlob)
	sort.Slice(files, func(i, j int) bool {
		return cpuIndex(filepath.Dir(filepath.Dir(files[i]))) < cpuIndex(filepath.Dir(filepath.Dir(files[j])))
	})
	topology := readCPUTopology()
	seenCores := map[int]bool{}

	nodes := make([]server.Node, 0, len(files))
	for _, file := range files {
		raw := readUint64(file)
		if raw == invalidUint64 {
			continue
		}

		cpu := cpuIndex(filepath.Dir(filepath.Dir(file)))
		label := "CPU Core #" + strconv.Itoa(cpu+1)
		sensorID := fmt.Sprintf("/cpu/clock/core-%d", cpu+1)
		if topo, ok := topology[cpu]; ok {
			if seenCores[topo.coreOrdinal] {
				continue
			}
			seenCores[topo.coreOrdinal] = true
			label = "CPU Core #" + strconv.Itoa(topo.coreOrdinal)
			sensorID = fmt.Sprintf("/cpu/clock/core-%d", topo.coreOrdinal)
		}

		value := float64(raw) / 1000.0
		min, max := trackValue(sensorID, value)
		valStr := formatValue(value, "MHz")
		minStr := formatValue(min, "MHz")
		maxStr := formatValue(max, "MHz")
		nodes = append(nodes, server.Node{
			Text:     label,
			Value:    valStr,
			Min:      minStr,
			Max:      maxStr,
			SensorId: sensorID,
			Type:     "Clock",
			RawValue: valStr,
			RawMin:   minStr,
			RawMax:   maxStr,
			ImageURL: "images/transparent.png",
		})
	}
	return nodes
}

func cpuLoadMetadata(cpu int, topology map[int]cpuTopology) (string, string) {
	if topo, ok := topology[cpu]; ok {
		if topo.threadCount > 1 {
			return fmt.Sprintf("CPU Core #%d Thread #%d", topo.coreOrdinal, topo.threadOrdinal),
				fmt.Sprintf("/cpu/load/core-%d-thread-%d", topo.coreOrdinal, topo.threadOrdinal)
		}
		return "CPU Core #" + strconv.Itoa(topo.coreOrdinal),
			fmt.Sprintf("/cpu/load/core-%d", topo.coreOrdinal)
	}
	return "CPU Core #" + strconv.Itoa(cpu+1), fmt.Sprintf("/cpu/load/core-%d", cpu+1)
}

func readCPUTopology() map[int]cpuTopology {
	dirs, _ := filepath.Glob(filepath.Join(cpuSysBase, "cpu[0-9]*"))
	if len(dirs) == 0 {
		return nil
	}

	entries := make([]cpuTopology, 0, len(dirs))
	for _, dir := range dirs {
		cpu := cpuIndex(dir)
		if cpu == math.MaxInt32 {
			continue
		}
		entries = append(entries, cpuTopology{
			cpu:       cpu,
			packageID: readTopologyInt(filepath.Join(dir, "topology/physical_package_id"), 0),
			coreID:    readTopologyInt(filepath.Join(dir, "topology/core_id"), cpu),
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].packageID != entries[j].packageID {
			return entries[i].packageID < entries[j].packageID
		}
		if entries[i].coreID != entries[j].coreID {
			return entries[i].coreID < entries[j].coreID
		}
		return entries[i].cpu < entries[j].cpu
	})

	out := make(map[int]cpuTopology, len(entries))
	coreOrdinal := 0
	for i := 0; i < len(entries); {
		j := i + 1
		for j < len(entries) &&
			entries[j].packageID == entries[i].packageID &&
			entries[j].coreID == entries[i].coreID {
			j++
		}

		coreOrdinal++
		threadCount := j - i
		for k := i; k < j; k++ {
			entry := entries[k]
			entry.coreOrdinal = coreOrdinal
			entry.threadOrdinal = (k - i) + 1
			entry.threadCount = threadCount
			out[entry.cpu] = entry
		}
		i = j
	}
	return out
}

func readTopologyInt(path string, fallback int) int {
	value := strings.TrimSpace(readFile(path))
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return n
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
	valStr := formatValue(value, "%")
	minStr := formatValue(min, "%")
	maxStr := formatValue(max, "%")
	return server.Node{
		Text:     label,
		Value:    valStr,
		Min:      minStr,
		Max:      maxStr,
		SensorId: sensorID,
		Type:     "Load",
		RawValue: valStr,
		RawMin:   minStr,
		RawMax:   maxStr,
		ImageURL: "images/transparent.png",
	}
}

func throughputNode(id, label string, value float64, sensorID string) server.Node {
	min, max := trackValue(id, value)
	valStr := formatBytes(value, true)
	minStr := formatBytes(min, true)
	maxStr := formatBytes(max, true)
	return server.Node{
		Text:     label,
		Value:    valStr,
		Min:      minStr,
		Max:      maxStr,
		SensorId: sensorID,
		Type:     "Throughput",
		RawValue: valStr,
		RawMin:   minStr,
		RawMax:   maxStr,
		ImageURL: "images/transparent.png",
	}
}

func dataNode(id, label string, value float64, sensorID string) server.Node {
	min, max := trackValue(id, value)
	valStr := formatBytes(value, false)
	minStr := formatBytes(min, false)
	maxStr := formatBytes(max, false)
	return server.Node{
		Text:     label,
		Value:    valStr,
		Min:      minStr,
		Max:      maxStr,
		SensorId: sensorID,
		Type:     "Data",
		RawValue: valStr,
		RawMin:   minStr,
		RawMax:   maxStr,
		ImageURL: "images/transparent.png",
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
