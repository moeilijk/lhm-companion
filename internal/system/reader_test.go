package system

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestReadAllIncludesCPUAndMemory(t *testing.T) {
	resetState()

	tmp := t.TempDir()
	procDir := filepath.Join(tmp, "proc")
	sysDir := filepath.Join(tmp, "sys")
	mustMkdirAll(t, procDir)
	mustMkdirAll(t, sysDir)

	writeFile(t, filepath.Join(procDir, "stat"), strings.Join([]string{
		"cpu  10 0 10 80 0 0 0 0 0 0",
		"cpu0 5 0 5 40 0 0 0 0 0 0",
		"cpu1 5 0 5 40 0 0 0 0 0 0",
	}, "\n"))
	writeFile(t, filepath.Join(procDir, "meminfo"), strings.Join([]string{
		"MemTotal:       1048576 kB",
		"MemAvailable:    524288 kB",
		"SwapTotal:       524288 kB",
		"SwapFree:        262144 kB",
	}, "\n"))
	writeFile(t, filepath.Join(sysDir, "devices/system/cpu/cpu0/cpufreq/scaling_cur_freq"), "2400000\n")
	writeFile(t, filepath.Join(sysDir, "devices/system/cpu/cpu1/cpufreq/scaling_cur_freq"), "2500000\n")
	writeFile(t, filepath.Join(sysDir, "devices/system/cpu/cpu0/topology/core_id"), "0\n")
	writeFile(t, filepath.Join(sysDir, "devices/system/cpu/cpu0/topology/physical_package_id"), "0\n")
	writeFile(t, filepath.Join(sysDir, "devices/system/cpu/cpu1/topology/core_id"), "1\n")
	writeFile(t, filepath.Join(sysDir, "devices/system/cpu/cpu1/topology/physical_package_id"), "0\n")

	procStatPath = filepath.Join(procDir, "stat")
	procMeminfo = filepath.Join(procDir, "meminfo")
	cpuFreqGlob = filepath.Join(sysDir, "devices/system/cpu/cpu[0-9]*/cpufreq/scaling_cur_freq")
	cpuSysBase = filepath.Join(sysDir, "devices/system/cpu")
	netClassPath = filepath.Join(sysDir, "class/net")
	blockClass = filepath.Join(sysDir, "class/block")
	now = func() time.Time { return time.Unix(10, 0) }

	nodes := ReadAll()
	if len(nodes) < 2 {
		t.Fatalf("nodes = %d, want at least 2", len(nodes))
	}

	if nodes[0].Text != "CPU" {
		t.Fatalf("first node = %q, want CPU", nodes[0].Text)
	}
	if got := nodes[1].Text; got != "Memory" {
		t.Fatalf("second node = %q, want Memory", got)
	}
}

func TestReadAllIncludesNetworkAndStorageRates(t *testing.T) {
	resetState()

	tmp := t.TempDir()
	procDir := filepath.Join(tmp, "proc")
	sysDir := filepath.Join(tmp, "sys")
	netDir := filepath.Join(sysDir, "class/net/eth0")
	blockDir := filepath.Join(sysDir, "class/block/nvme0n1")

	writeFile(t, filepath.Join(procDir, "stat"), "cpu  10 0 10 80 0 0 0 0 0 0\n")
	writeFile(t, filepath.Join(procDir, "meminfo"), "MemTotal: 1024 kB\nMemAvailable: 512 kB\n")
	writeFile(t, filepath.Join(netDir, "statistics/rx_bytes"), "1000\n")
	writeFile(t, filepath.Join(netDir, "statistics/tx_bytes"), "2000\n")
	writeFile(t, filepath.Join(netDir, "speed"), "1000\n")
	writeFile(t, filepath.Join(blockDir, "stat"), "1 0 2 0 3 0 4 0 0 5 0\n")
	writeFile(t, filepath.Join(blockDir, "queue/logical_block_size"), "512\n")
	writeFile(t, filepath.Join(blockDir, "device/model"), "FastDisk\n")

	procStatPath = filepath.Join(procDir, "stat")
	procMeminfo = filepath.Join(procDir, "meminfo")
	cpuFreqGlob = filepath.Join(sysDir, "devices/system/cpu/cpu[0-9]*/cpufreq/scaling_cur_freq")
	netClassPath = filepath.Join(sysDir, "class/net")
	blockClass = filepath.Join(sysDir, "class/block")

	now = func() time.Time { return time.Unix(10, 0) }
	_ = ReadAll()

	writeFile(t, filepath.Join(netDir, "statistics/rx_bytes"), "3000\n")
	writeFile(t, filepath.Join(netDir, "statistics/tx_bytes"), "5000\n")
	writeFile(t, filepath.Join(blockDir, "stat"), "1 0 6 0 3 0 10 0 0 15 0\n")
	now = func() time.Time { return time.Unix(12, 0) }

	nodes := ReadAll()
	if len(nodes) < 4 {
		t.Fatalf("nodes = %d, want at least 4", len(nodes))
	}

	var network, storageFound bool
	for _, node := range nodes {
		if node.Text == "Network" {
			network = true
		}
		if node.Text == "Storage" {
			storageFound = true
		}
	}
	if !network {
		t.Fatal("Network node missing")
	}
	if !storageFound {
		t.Fatal("Storage node missing")
	}
}

func TestBuildCPULoadNodesUseLHMThreadLabels(t *testing.T) {
	resetState()

	tmp := t.TempDir()
	sysDir := filepath.Join(tmp, "sys")
	writeFile(t, filepath.Join(sysDir, "devices/system/cpu/cpu0/topology/core_id"), "0\n")
	writeFile(t, filepath.Join(sysDir, "devices/system/cpu/cpu0/topology/physical_package_id"), "0\n")
	writeFile(t, filepath.Join(sysDir, "devices/system/cpu/cpu1/topology/core_id"), "0\n")
	writeFile(t, filepath.Join(sysDir, "devices/system/cpu/cpu1/topology/physical_package_id"), "0\n")
	writeFile(t, filepath.Join(sysDir, "devices/system/cpu/cpu2/topology/core_id"), "1\n")
	writeFile(t, filepath.Join(sysDir, "devices/system/cpu/cpu2/topology/physical_package_id"), "0\n")
	writeFile(t, filepath.Join(sysDir, "devices/system/cpu/cpu3/topology/core_id"), "1\n")
	writeFile(t, filepath.Join(sysDir, "devices/system/cpu/cpu3/topology/physical_package_id"), "0\n")
	cpuSysBase = filepath.Join(sysDir, "devices/system/cpu")

	current := map[string]cpuTimes{
		"cpu":  {total: 100, idle: 50},
		"cpu0": {total: 100, idle: 50},
		"cpu1": {total: 100, idle: 50},
		"cpu2": {total: 100, idle: 50},
		"cpu3": {total: 100, idle: 50},
	}

	nodes := buildCPULoadNodes(current)
	got := make([]string, 0, len(nodes))
	for _, node := range nodes {
		got = append(got, node.Text)
	}

	want := []string{
		"CPU Total",
		"CPU Core #1 Thread #1",
		"CPU Core #1 Thread #2",
		"CPU Core #2 Thread #1",
		"CPU Core #2 Thread #2",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("labels = %#v, want %#v", got, want)
	}
}

func TestReadCPUClockNodesDeduplicateSMTSiblings(t *testing.T) {
	resetState()

	tmp := t.TempDir()
	sysDir := filepath.Join(tmp, "sys")
	writeFile(t, filepath.Join(sysDir, "devices/system/cpu/cpu0/cpufreq/scaling_cur_freq"), "2400000\n")
	writeFile(t, filepath.Join(sysDir, "devices/system/cpu/cpu1/cpufreq/scaling_cur_freq"), "2500000\n")
	writeFile(t, filepath.Join(sysDir, "devices/system/cpu/cpu2/cpufreq/scaling_cur_freq"), "2600000\n")
	writeFile(t, filepath.Join(sysDir, "devices/system/cpu/cpu3/cpufreq/scaling_cur_freq"), "2700000\n")
	writeFile(t, filepath.Join(sysDir, "devices/system/cpu/cpu0/topology/core_id"), "0\n")
	writeFile(t, filepath.Join(sysDir, "devices/system/cpu/cpu0/topology/physical_package_id"), "0\n")
	writeFile(t, filepath.Join(sysDir, "devices/system/cpu/cpu1/topology/core_id"), "0\n")
	writeFile(t, filepath.Join(sysDir, "devices/system/cpu/cpu1/topology/physical_package_id"), "0\n")
	writeFile(t, filepath.Join(sysDir, "devices/system/cpu/cpu2/topology/core_id"), "1\n")
	writeFile(t, filepath.Join(sysDir, "devices/system/cpu/cpu2/topology/physical_package_id"), "0\n")
	writeFile(t, filepath.Join(sysDir, "devices/system/cpu/cpu3/topology/core_id"), "1\n")
	writeFile(t, filepath.Join(sysDir, "devices/system/cpu/cpu3/topology/physical_package_id"), "0\n")

	cpuFreqGlob = filepath.Join(sysDir, "devices/system/cpu/cpu[0-9]*/cpufreq/scaling_cur_freq")
	cpuSysBase = filepath.Join(sysDir, "devices/system/cpu")

	nodes := readCPUClockNodes()
	got := make([]string, 0, len(nodes))
	for _, node := range nodes {
		got = append(got, node.Text)
	}

	want := []string{
		"CPU Core #1",
		"CPU Core #2",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("labels = %#v, want %#v", got, want)
	}
}

func resetState() {
	trackMu.Lock()
	tracking = map[string]*tracker{}
	trackMu.Unlock()

	cpuPrevMu.Lock()
	cpuPrev = map[string]cpuTimes{}
	cpuPrevMu.Unlock()

	netPrevMu.Lock()
	netPrev = map[string]netSample{}
	netPrevMu.Unlock()

	blockPrevMu.Lock()
	blockPrev = map[string]blockSample{}
	blockPrevMu.Unlock()

	procStatPath = "/proc/stat"
	procMeminfo = "/proc/meminfo"
	cpuFreqGlob = "/sys/devices/system/cpu/cpu[0-9]*/cpufreq/scaling_cur_freq"
	cpuSysBase = "/sys/devices/system/cpu"
	netClassPath = "/sys/class/net"
	blockClass = "/sys/class/block"
	now = time.Now
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	mustMkdirAll(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}
