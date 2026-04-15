package hwmon

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/moeilijk/lhm-companion/internal/server"
)

func TestReadAllUsesStableCPUAndLPCIDs(t *testing.T) {
	resetHWMonState(t)

	tmp := t.TempDir()
	sysfsBase = filepath.Join(tmp, "class/hwmon")

	createHWMonDevice(t, tmp, "hwmon0", "coretemp", "devices/platform/coretemp.0", map[string]string{
		"temp1_input": "42000\n",
	})
	createHWMonDevice(t, tmp, "hwmon1", "nct6798", "devices/platform/nct6798d.656", map[string]string{
		"fan1_input": "1234\n",
	})

	nodes := ReadAll()
	ids := leafSensorIDs(nodes)

	assertContainsID(t, ids, "/cpu/temperature/1")
	assertContainsID(t, ids, "/lpc/nct6798d.656/fan/1")
	assertNoVolatileHWMonIDs(t, ids)
}

func TestReadAllUsesStableStorageAndNetworkIDs(t *testing.T) {
	resetHWMonState(t)

	tmp := t.TempDir()
	sysfsBase = filepath.Join(tmp, "class/hwmon")

	createHWMonDevice(t, tmp, "hwmon0", "drivetemp", "devices/pci0000:00/0000:00:17.0/ata1/host0/target0:0:0/0:0:0:0/block/sda", map[string]string{
		"temp1_input": "31800\n",
	})
	createHWMonDevice(t, tmp, "hwmon1", "r8169", "devices/pci0000:00/0000:00:1f.6/net/enp0s31f6", map[string]string{
		"temp1_input": "27500\n",
	})
	createHWMonDevice(t, tmp, "hwmon2", "nvme", "devices/pci0000:00/0000:04:00.0/nvme/nvme0/nvme0n1", map[string]string{
		"temp1_input": "30100\n",
	})

	nodes := ReadAll()
	ids := leafSensorIDs(nodes)

	assertContainsID(t, ids, "/storage/sda/temperature/1")
	assertContainsID(t, ids, "/network/enp0s31f6/temperature/1")
	assertContainsID(t, ids, "/storage/nvme0n1/temperature/1")
	assertNoVolatileHWMonIDs(t, ids)
}

func TestReadAllNormalizesCPUCoreTempLabels(t *testing.T) {
	resetHWMonState(t)

	tmp := t.TempDir()
	sysfsBase = filepath.Join(tmp, "class/hwmon")

	createHWMonDevice(t, tmp, "hwmon0", "coretemp", "devices/platform/coretemp.0", map[string]string{
		"temp1_input": "45000\n",
		"temp1_label": "Package id 0\n",
		"temp2_input": "41000\n",
		"temp2_label": "Core 0\n",
		"temp3_input": "42000\n",
		"temp3_label": "Core 1\n",
	})

	labels := leafLabelsByID(ReadAll())
	if got := labels["/cpu/temperature/1"]; got != "CPU Package" {
		t.Fatalf("label /cpu/temperature/1 = %q, want %q", got, "CPU Package")
	}
	if got := labels["/cpu/temperature/2"]; got != "CPU Core #1" {
		t.Fatalf("label /cpu/temperature/2 = %q, want %q", got, "CPU Core #1")
	}
	if got := labels["/cpu/temperature/3"]; got != "CPU Core #2" {
		t.Fatalf("label /cpu/temperature/3 = %q, want %q", got, "CPU Core #2")
	}
}

func resetHWMonState(t *testing.T) {
	t.Helper()

	oldSysfsBase := sysfsBase
	t.Cleanup(func() {
		sysfsBase = oldSysfsBase
		trackMu.Lock()
		tracking = map[string]*tracker{}
		trackMu.Unlock()
	})

	trackMu.Lock()
	tracking = map[string]*tracker{}
	trackMu.Unlock()
}

func createHWMonDevice(t *testing.T, root, hwmonName, name, deviceRel string, files map[string]string) {
	t.Helper()

	hwmonDir := filepath.Join(root, "class/hwmon", hwmonName)
	deviceDir := filepath.Join(root, deviceRel)
	if err := os.MkdirAll(hwmonDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", hwmonDir, err)
	}
	if err := os.MkdirAll(deviceDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", deviceDir, err)
	}
	if err := os.WriteFile(filepath.Join(hwmonDir, "name"), []byte(name+"\n"), 0o644); err != nil {
		t.Fatalf("write name: %v", err)
	}
	for rel, content := range files {
		path := filepath.Join(hwmonDir, rel)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	if err := os.Symlink(deviceDir, filepath.Join(hwmonDir, "device")); err != nil {
		t.Fatalf("symlink device: %v", err)
	}
}

func leafSensorIDs(nodes []server.Node) []string {
	var ids []string
	var walk func(server.Node)
	walk = func(n server.Node) {
		if n.SensorId != "" {
			ids = append(ids, n.SensorId)
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	for _, node := range nodes {
		walk(node)
	}
	slices.Sort(ids)
	return ids
}

func leafLabelsByID(nodes []server.Node) map[string]string {
	labels := map[string]string{}
	var walk func(server.Node)
	walk = func(n server.Node) {
		if n.SensorId != "" {
			labels[n.SensorId] = n.Text
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	for _, node := range nodes {
		walk(node)
	}
	return labels
}

func assertContainsID(t *testing.T, ids []string, want string) {
	t.Helper()
	for _, id := range ids {
		if id == want {
			return
		}
	}
	t.Fatalf("sensor ids = %v, want %q", ids, want)
}

func assertNoVolatileHWMonIDs(t *testing.T, ids []string) {
	t.Helper()
	for _, id := range ids {
		if strings.Contains(id, "/hwmon") {
			t.Fatalf("sensor id %q still depends on hwmonN", id)
		}
	}
}
