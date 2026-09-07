# Thermal

CPU/GPU benchmarks, temperature recordings, and before/after comparisons—with PDF findings and PNG reports. For computers auditioning to become space heaters.

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

If Windows installation fails with `Get-FileHash is not recognized`, the downloaded installer requires a cmdlet unavailable in your PowerShell host. The corrected [repository installer](install.ps1) uses .NET SHA-256 hashing directly. From the repository root, run it with the release tag you want to install, for example:

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\install.ps1 -Version v0.6.0
```

Release downloads only receive this fix when an updated installer is published. You can also download and extract the Windows archive as described below.

### 2. Download a prebuilt GitHub release

Grab an archive from [GitHub Releases](https://github.com/preacherxp/thermal/releases/latest) and extract it. Windows, Linux, and macOS support **x64 (`amd64`)** and **ARM64 (`arm64`)**; macOS assets use the name `darwin`.

Run `./thermal` on macOS/Linux or `.\thermal.exe` in PowerShell. Use `chmod +x thermal` if your extraction tool drops executable permissions.

## Run

```sh
thermal
```

Benchmarks CPU, then GPU, for **30 seconds each**. No survey prompts. Saves JSON, PDF, and PNG reports and prints their paths. The PDF uses a minimal A4 layout with CPU/GPU scores, thermal charts, evidence-based findings, and a suggested next step.

| Want to… | Command |
| --- | --- |
| Check available sensors | `thermal doctor` |
| Show full console tables | `thermal --verbose` |
| Test just the CPU | `thermal --target cpu` |
| Change each test's duration | `thermal --duration 60s` |
| Monitor a running game without adding load | `thermal record --survey skip --workload my-game --duration 120s` |
| Compare saved runs | `thermal compare before.json after.json --pdf comparison.pdf --png comparison.png` |
| Try a synthetic report without load | `thermal demo --pdf demo.pdf --png demo.png` |
| See all options | `thermal --help` |

Interactive terminals open a modern live dashboard built with Bubble Tea, Lip Gloss, and Bubbles: CPU/GPU cards, temperature traces, progress, and saved report paths. It adapts to the window size and returns to the prompt when reports are saved. Press `q`, Esc, or Ctrl+C to stop safely and save partial results.

Use `--no-tui` for minimal output, or `--verbose` / `thermal report run.json` for full tables. Pipes, `--json`, unsupported terminals, `NO_COLOR`, and `TERM=dumb` automatically use the minimal fallback.

PNG reports show up to six sensor charts per phase, prioritizing the CPU/GPU targets. An omission note identifies larger sensor sets; the saved JSON and `thermal report run.json` retain every reading. PDF findings assess CPU and GPU sensors separately for passive recordings.

[See a sample PNG](examples/comparison.png) · [More examples](RUN.md) · [Automation guide](AGENTS.md)

Use `--out run.json` to choose an output path, `--json` for JSON stdout, `--pdf report.pdf` for a custom PDF path, or `--no-pdf` / `--no-png` to skip either export. Existing reports are never overwritten. Ctrl+C saves partial results.

## Know before you toast

- Benchmarks require a readable target temperature and default to a **90 °C stop limit**. Missing monitoring refuses that test; `--allow-unmonitored` explicitly permits load without it.
- **macOS uses native AppleSMC temperature readings**, without sudo or extra tools. Apple Silicon monitors the CPU die and integrated GPU; Intel CPU monitoring depends on exposed SMC keys. Intel/discrete GPU sensor identity is not inferred, so those GPU tests still require a matching provider or explicit unmonitored mode. Missing sensors remain unknown.
- **Apple Silicon CPU/GPU wattage works natively through IOReport**, without sudo, macmon, or opt-in flags. Values are interval-average estimates from Apple’s Energy Model, not whole-computer wall power. Missing counters stay unknown, and overlapping domain/die totals are never added together.
- **Windows CPU temperatures require a provider.** Optional providers include LibreHardwareMonitor WMI on Windows and macmon on Apple Silicon for additional telemetry. Configure your chosen provider separately, then set `THERMAL_EXTERNAL_PROVIDERS=1`; nothing installs automatically.
- **GPU benchmarking uses the system OpenCL framework on macOS and an installed OpenCL GPU driver on Windows.** Other platforms mark it unavailable. Results are verified on the CPU; scores measure compute throughput, not gaming FPS. Apple has deprecated OpenCL, so a missing framework or unsupported device is reported explicitly.
- Refused or unavailable tests stay in the report; partial suites return exit code 1. Missing readings stay unknown. Guessing is not a sensor.
- Compare the same workload under similar conditions. Temperature alone cannot diagnose a cooling fault, and software limits do not replace hardware protection.

## Development

Go 1.26.8+, with pinned Go TUI dependencies and no cgo. From the repository root:

```sh
go mod download
go test ./...
go vet ./...
go run ./scripts/dist check-deps
go run ./scripts/dist build --local
```

Omit `--local` to build all six targets. `go run ./scripts/dist package` creates archives, installers, and checksums in a fresh `release/v<version>/` directory. CI publishes releases when a matching version tag is pushed.

See [AGENTS.md](AGENTS.md#development) for native and installer checks.
