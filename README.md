# Thermal

CPU/GPU benchmarks, temperature recordings, and before/after comparisons—with PNG reports. For computers auditioning to become space heaters.

One standalone binary. No runtime to install. Your data stays local.

## Install

### 1. Install with curl

**macOS / Linux**

```sh
curl -fsSL https://github.com/preacherxp/thermal/releases/latest/download/install.sh | sh
```

Installs to `~/.local/bin`. Add it to PATH if needed:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

**Windows (PowerShell)**

```powershell
curl.exe -fsSL https://github.com/preacherxp/thermal/releases/latest/download/install.ps1 -o install.ps1
if ($LASTEXITCODE -eq 0) { powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\install.ps1 }
```

Installs to `%LOCALAPPDATA%\Thermal\bin`. Add that directory to PATH or use the full path printed by the installer.

Both installers select your architecture and verify SHA-256 checksums. No administrator access required.

### 2. Download a prebuilt GitHub release

Grab an archive from [GitHub Releases](https://github.com/preacherxp/thermal/releases/latest) and extract it. Windows, Linux, and macOS support **x64 (`amd64`)** and **ARM64 (`arm64`)**; macOS assets use the name `darwin`.

Run `./thermal` on macOS/Linux or `.\thermal.exe` in PowerShell. Use `chmod +x thermal` if your extraction tool drops executable permissions.

## Run

```sh
thermal
```

Benchmarks CPU, then GPU, for **30 seconds each**. No survey prompts. Saves JSON and PNG reports and prints their paths.

| Want to… | Command |
| --- | --- |
| Check available sensors | `thermal doctor` |
| Test just the CPU | `thermal --target cpu` |
| Change each test's duration | `thermal --duration 60s` |
| Monitor a running game without adding load | `thermal record --survey skip --workload my-game --duration 120s` |
| Compare saved runs | `thermal compare before.json after.json --png comparison.png` |
| Try a synthetic report without load | `thermal demo --png demo.png` |
| See all options | `thermal --help` |

[See a sample PNG](examples/comparison.png) · [More examples](RUN.md) · [Automation guide](AGENTS.md)

Use `--out run.json` to choose an output path, `--json` for JSON stdout, or `--no-png` to skip the image. Existing reports are never overwritten. Ctrl+C saves partial results.

## Know before you toast

- Benchmarks require a readable target temperature and default to a **90 °C stop limit**. Missing monitoring refuses that test; `--allow-unmonitored` explicitly permits load without it.
- **Windows/macOS CPU temperatures are unavailable by default.** Optional providers include LibreHardwareMonitor WMI on Windows and macmon on Apple Silicon. Configure your chosen provider separately, then set `THERMAL_EXTERNAL_PROVIDERS=1`; nothing installs automatically.
- **GPU benchmarking currently requires Windows and an OpenCL GPU driver.** Other platforms mark it unavailable. Scores measure compute throughput, not gaming FPS.
- Refused or unavailable tests stay in the report; partial suites return exit code 1. Missing readings stay unknown. Guessing is not a sensor.
- Compare the same workload under similar conditions. Temperature alone cannot diagnose a cooling fault, and software limits do not replace hardware protection.

## Development

Go 1.23+, standard library only, no cgo. From the repository root:

```sh
go test ./...
go vet ./...
go run ./scripts/dist check-deps
go run ./scripts/dist build --local
```

Omit `--local` to build all six targets. `go run ./scripts/dist package` creates archives, installers, and checksums in a fresh `release/v<version>/` directory. CI publishes releases when a matching version tag is pushed.

See [AGENTS.md](AGENTS.md#development) for native and installer checks.
