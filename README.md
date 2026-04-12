# lhm-companion

Linux sensor bridge for [lhm-streamdeck](https://github.com/moeilijk/lhm-streamdeck).

Exposes `/sys/class/hwmon` data (and optionally NVIDIA GPU data) as a `data.json` endpoint compatible with Libre Hardware Monitor. The Stream Deck plugin connects to it as a remote source profile.

## How it works

The Stream Deck plugin's `lhm-bridge.exe` reads `http://host:port/data.json`. lhm-companion serves exactly that format — no modifications to the plugin required. Add a source profile in the Stream Deck plugin settings pointing to the Linux machine's IP and port.

## Requirements

- Linux with `/sys/class/hwmon` (kernel ≥ 4.x — any modern distro)
- Go 1.22+ (to build from source)
- `nvidia-smi` on `$PATH` (optional, for NVIDIA GPU readings)
- hwmon read access — see [Permissions](#permissions)

## Install

### From source

```sh
git clone https://github.com/moeilijk/lhm-companion
cd lhm-companion
make build
sudo make install        # copies binary + systemd unit
sudo systemctl enable --now lhm-companion
```

### Manual

```sh
go install github.com/moeilijk/lhm-companion/cmd/lhm-companion@latest
```

## Usage

```
lhm-companion [flags]

  -port int     port to listen on (default 8085, env: LHM_PORT)
  -nvidia       include nvidia-smi GPU readings (auto-detected)
  -version      print version and exit
```

The service binds to `0.0.0.0:<port>` — ensure your firewall allows inbound TCP on that port from the Windows machine.

## Permissions

hwmon files are world-readable on most distros. If not:

```sh
# Option 1: add your user to the 'video' or 'sensors' group
sudo usermod -aG video $USER

# Option 2: udev rule (distro-independent)
echo 'SUBSYSTEM=="hwmon", ACTION=="add", RUN+="/bin/chmod -R a+r /sys/class/hwmon/%k/"' \
  | sudo tee /etc/udev/rules.d/99-hwmon.rules
sudo udevadm trigger
```

When running as a systemd service, add `User=` to the unit or use the udev rule.

## Stream Deck plugin setup

1. Open the **Settings** tile in Stream Deck
2. Under **Source Profiles**, click **Add**
3. Set **Name**, **Host** (Linux machine IP), **Port** (default 8085)
4. Sensor tiles now show a **Source** dropdown — select the new profile

## Sensor coverage

| Source | Data |
|--------|------|
| `/sys/class/hwmon` | CPU temp, fan RPM, voltages, power, clocks (chipset-dependent) |
| `nvidia-smi` | GPU temp, fan %, core/memory load, power draw, clocks |

AMD GPU readings come through hwmon via the `amdgpu` driver automatically.

## License

MIT
