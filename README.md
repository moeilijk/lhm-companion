# lhm-companion

Linux sensor bridge for [lhm-streamdeck](https://github.com/moeilijk/lhm-streamdeck).

Serves CPU load, memory, network, storage, hardware temperatures, fan speeds, voltages and more as a `data.json` endpoint that is fully compatible with Libre Hardware Monitor. The Stream Deck plugin connects to it as a remote source profile — no modifications to the plugin required.

## How it works

The Stream Deck plugin can poll a remote `http://host:port/data.json` endpoint. lhm-companion serves that endpoint in the exact JSON format Libre Hardware Monitor produces. Add a source profile in the plugin settings pointing to the Linux machine's IP and port; all sensor tiles will then offer that source in their dropdown.

## Requirements

- Linux (any modern distro — kernel ≥ 4.x)
- Go 1.26+ (source builds only)
- `nvidia-smi` on `$PATH` (optional — for NVIDIA GPU readings)
- Read access to `/sys/class/hwmon` (optional — for hardware sensor readings; world-readable on most distros)

CPU load, memory, network and storage metrics are read from `/proc` and `/sys` and require no special permissions.

## Install

### Binary install (no Go required)

```sh
curl -fsSL https://github.com/moeilijk/lhm-companion/releases/latest/download/install.sh | sudo sh
```

The script installs the binary to `/usr/local/bin` and registers a systemd service that starts automatically on boot.

To pin a specific version (replace `v0.1.4` with the desired tag):

```sh
curl -fsSL https://github.com/moeilijk/lhm-companion/releases/download/v0.1.4/install.sh | sudo env VERSION=v0.1.4 sh
```

### Source install (Go required)

```sh
git clone https://github.com/moeilijk/lhm-companion
cd lhm-companion
make build
sudo make install
```

## Uninstall

```sh
curl -fsSL https://github.com/moeilijk/lhm-companion/releases/latest/download/uninstall.sh | sudo sh
```

## Usage

```
lhm-companion [flags]

  -port int     port to listen on (default 8085, env: LHM_PORT)
  -nvidia       include nvidia-smi GPU readings (auto-detected)
  -version      print version and exit
```

The service binds to `0.0.0.0:<port>` — ensure your firewall allows inbound TCP on that port from the Windows machine.

## Stream Deck plugin setup

1. Open the **Settings** tile in Stream Deck
2. Under **Source Profiles**, click **Add**
3. Set **Name**, **Host** (Linux machine IP), **Port** (default 8085)
4. Sensor tiles now show a **Source** dropdown — select the new profile

## Sensor coverage

### System metrics

Read on every poll from `/proc` and `/sys`; no drivers or extra permissions required.

| Source | Sensor type | Sensors |
|--------|-------------|---------|
| `/proc/stat` | Load | CPU Total, per-core %, and on SMT systems also `CPU Core #… Thread #…` |
| `/sys/…/cpufreq/scaling_cur_freq` | Clock | CPU Core #1…N, deduplicated to physical cores via sysfs topology (requires `cpufreq` driver; not available in WSL) |
| `/proc/meminfo` | Load / Data | RAM load %, Used / Available / Total RAM, Used / Total Swap |
| `/sys/class/net/*/statistics` | Throughput / Data / Load | Rx/Tx rate shown in base `B/s`, cumulative totals shown in base `B`, plus link utilisation % when link speed is known; loopback (`lo`) excluded |
| `/sys/class/block/*/stat` | Throughput / Load / Data | Read/Write rate, I/O activity %, cumulative read/write bytes; whole disks only — partitions, loop, ram and zram devices excluded |

### Hardware sensors

| Source | Sensor type | Notes |
|--------|-------------|-------|
| `/sys/class/hwmon` | Temperature, Fan, Voltage, Power, Current, Clock | Reads every `hwmon*` device; works with `coretemp`, `k10temp`, `nct6*`, `it87*`, `amdgpu`, etc. |
| `nvidia-smi` *(optional)* | Temperature, Control, Load, Power, Clock | GPU Core temp; fan duty-cycle % (type `Control`); Core & Memory load; package power with TDP as Max; Core & Memory clocks |

AMD GPU sensors (temp, fan, power, clocks) come through hwmon via the `amdgpu` driver automatically — no extra flags needed.

When `--nvidia` is active (auto-detected if `nvidia-smi` is on `$PATH`), the kernel's `nvidia` hwmon device is skipped so GPU temperature is not reported twice.

## License

Copyright (C) 2026 cvdveer

This program is free software: you can redistribute it and/or modify it under
the terms of the [GNU General Public License v3.0](LICENSE) as published by the
Free Software Foundation.

This program is distributed in the hope that it will be useful, but **without
any warranty**; without even the implied warranty of merchantability or fitness
for a particular purpose. See the GNU General Public License for details.
