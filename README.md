# Thermal

A small, self-contained **Go CLI** for measuring computer thermals, tracking interventions, and comparing before/after runs. No language runtime or third-party Go modules are required.

## Install

Choose one of two ways to get Thermal: install with curl, or download a prebuilt GitHub release.

### 1. Install with curl

On macOS or Linux:

```sh
curl -fsSL https://github.com/preacherxp/thermal/releases/latest/download/install.sh | sh
```

The installer selects your OS and architecture, verifies the release archive's SHA-256 checksum, and installs `thermal` in `~/.local/bin`. Add that directory to your shell's PATH if needed:

```sh
export PATH="$HOME/.local/bin:$PATH"
thermal --help
```

It requires curl, tar, and either sha256sum or shasum. It does not need root access or change your shell configuration. Set `THERMAL_INSTALL_DIR` to choose another directory. Re-running the installer upgrades the executable after verification.

On Windows (PowerShell):

```powershell
curl.exe -fsSL https://github.com/preacherxp/thermal/releases/latest/download/install.ps1 -o install.ps1
if ($LASTEXITCODE -eq 0) { powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\install.ps1 }
```

The Windows installer verifies the ZIP checksum and installs `thermal.exe` in `%LOCALAPPDATA%\Thermal\bin`. Add that directory to your user PATH, or run the full path printed by the installer. Use `-InstallDir` to choose another directory. No administrator access is required.

To install a specific release, download its installer instead of `latest`:

```sh
curl -fsSL https://github.com/preacherxp/thermal/releases/download/v0.4.0/install.sh | sh
```

Each published installer defaults to its own release tag, so the archive and checksums come from the same version. `THERMAL_VERSION` overrides the tag; `THERMAL_REPOSITORY` supports forks.

### 2. Download a prebuilt GitHub release

Open [GitHub Releases](https://github.com/preacherxp/thermal/releases/latest), download the archive for your system, and extract it:

| System | Release asset |
| --- | --- |
| Windows, x64 | `thermal-windows-amd64.zip` |
| Windows, ARM64 | `thermal-windows-arm64.zip` |
| Linux, x64 | `thermal-linux-amd64.tar.gz` |
| Linux, ARM64 | `thermal-linux-arm64.tar.gz` |
| macOS, Intel | `thermal-darwin-amd64.tar.gz` |
| macOS, Apple Silicon | `thermal-darwin-arm64.tar.gz` |

Archives contain the standalone executable, documentation, license, and third-party notices. `checksums.txt` accompanies every release. Compare the archive hash with its entry using `sha256sum FILE` on Linux, `shasum -a 256 FILE` on macOS, or `Get-FileHash FILE -Algorithm SHA256` on Windows.

Run `./thermal --help` on macOS/Linux, or `.\thermal.exe --help` in PowerShell, from the extracted folder. You can move the executable into a directory on PATH to use `thermal` anywhere. Tar archives preserve executable permissions; use `chmod +x thermal` if your extraction tool drops them.

## Run

Run CPU and GPU benchmarks and create PNG results with one command:

```sh
thermal
```

This runs the CPU test for 30 seconds, then the GPU test for 30 seconds, without survey prompts. It saves one JSON file and one PNG containing separate scores, thermal timelines, and test statuses, then prints their paths. Use `thermal --duration 60s` to change the duration of each test, `thermal --target cpu` or `thermal --target gpu` for one component, and `thermal --json` for machine-readable stdout.

The GPU benchmark uses a real OpenCL compute kernel on a Windows GPU through the installed graphics driver; no CPU/software fallback is used. It prefers a discrete GPU and records the selected device. Other platforms currently report the GPU test as unavailable. Scores count verified integer iterations per second, not graphics FPS or third-party benchmark points.

Each target requires a readable temperature. Native Windows/macOS CPU temperature is unavailable by default, so that phase is refused unless an optional provider is configured or you explicitly use `thermal --allow-unmonitored`. A monitored GPU test can still run when CPU monitoring is unavailable. Refused/unavailable phases appear in the PNG and JSON; a partial suite returns exit code 1. No scores are invented for tests that did not run.

For passive monitoring without load, use `thermal record --survey skip --duration 30s`. Use `thermal --help` for all commands.

Start with [RUN.md](RUN.md) for simple commands and PNG examples, or [AGENTS.md](AGENTS.md) for agent usage.

## PNG reports

`record`, `benchmark`, and `import` automatically save a PNG beside their JSON output.
Reports include temperature timelines, sustained measurements, whole-run peak
temperatures, throttling counts, warnings, and next steps. Comparisons show
before/after values and changes. Missing readings appear as unknown.

[View a sample comparison PNG](examples/comparison.png).

```sh
thermal demo --png demo.png
thermal report after.json --png report.png
thermal compare before.json after.json --png comparison.png
thermal record --duration 30s --out run.json --png custom.png
```

Use `--no-png` on `record`, `benchmark`, or `import` to disable automatic export.
Exports never overwrite existing files. PNG status goes to stderr, so `--json`
stdout remains valid JSON. A PNG failure leaves saved JSON available for another
export attempt. Rendering uses Go's standard library and an embedded Go font atlas;
no browser, image converter, or installed font is needed. See
`internal/thermal/assets/LICENSE` for the font license. Unsupported font characters
display as `?`; original text remains in the JSON. Very large reports exceeding
24,000 pixels in height return an error; the text and JSON reports remain available.

## Workflow

Run `thermal doctor` first. Record your power source and profile rather than relying on defaults.
Keep workload, duration, worker count, room temperature and starting temperature as
similar as practical. Let the device cool between tests. Change one thing at a time.

```sh
thermal benchmark --stage baseline --duration 60s --workers 4 --profile balanced --power ac --ambient 22 --out before.json

# Save your work and close unneeded busy apps, then repeat.
thermal benchmark --stage apps-closed --duration 60s --workers 4 --profile balanced --power ac --ambient 22 --out after.json

thermal compare before.json after.json
thermal report after.json
thermal history
```

With no subcommand, options apply to CPU and GPU benchmarks with the survey skipped.
With an explicit subcommand, options follow the command. For `import`, flags precede the CSV path.
`report`, `compare`, and `demo` accept `--png FILE.png`; flags may precede or follow paths.
`compare` also accepts `--json`, including together with `--png`.

Intervention stages:

| Stage | What changed |
| --- | --- |
| baseline | Original measurement |
| apps-closed | Unneeded applications closed |
| profile-changed | OS/OEM power or thermal profile changed |
| fans-cleaned | Airflow checked and dust/fans/fins cleaned |
| repasted | Thermal interface serviced |
| specialist | Device inspected or repaired professionally |

These labels record what **you actually did**. Thermal does not close apps, switch
profiles, change fans, or make hardware modifications automatically. Recommendations
advance through those stages when readings warrant further investigation; a cool
run does not automatically recommend repasting. Temperature alone cannot diagnose
paste or heatsink contact. Shutdowns or a failed fan warrant specialist help.

### Record a game or GPU workload

The default benchmark runs CPU SHA-256 and GPU integer compute tests in separate
phases. To investigate a particular game, renderer or external GPU workload, start
that workload and record it:

```sh
thermal record --workload my-game-fixed-scene --duration 120s --stage baseline --profile performance --power ac --out before-gpu.json
thermal record --workload my-game-fixed-scene --duration 120s --stage fans-cleaned --profile performance --power ac --out after-gpu.json
thermal compare before-gpu.json after-gpu.json
```

Reports show mean/peak temperatures and available clocks, power and hardware
thermal-throttling flags. Comparisons use the **last 25% of captured samples by elapsed
time**, matching devices by ID. CPU benchmark throughput (SHA-256 hashes per second)
is compared separately over the whole load period. It is a repeatable local workload,
not a general CPU ranking.

A GPU may reach the same temperature after cleaning while sustaining more clock
speed and power. The report preserves those measurements. It warns about changed
workloads, machine IDs, profiles, power source, room temperature, CPU load,
GPU utilization, sample intervals, sparse data and early stops.
A comparison is evidence to review, not proof of causality.

### Benchmark limits

Default: 30 seconds per target (CPU then GPU), up to four CPU workers, one-second
requested sampling interval, and a 90 C target stop limit. Maximum duration is
30 minutes per target; worker count is limited
to the number of logical CPUs. `--stop-temp` accepts 50–100 C. If a Linux sensor
exposes a critical limit, its critical value minus 5 C is used when lower.

A benchmark phase refuses to start without its target temperature and stops if readings disappear.
`--allow-unmonitored` explicitly bypasses the missing-sensor rule, while retaining
time and any available temperature limits. macmon reports an average CPU temperature,
which can be below the hottest core.

Monitoring is best effort, **not a replacement for hardware thermal protection**.
External provider commands have timeouts; actual sample intervals can exceed the requested
interval. Sensors can also be stale or incomplete. Ctrl+C cancels the CPU workers,
joins them, and saves partial results. Refused/stopped/interrupted runs return a
nonzero exit code and preserve a report. No long stress test is needed for the demo.

## Platform support

The native executable has **zero third-party Go modules, no cgo, no required helper
apps, and no language runtime to install**. Copy the binary for your OS/architecture
and run it. Windows/macOS still use their OS libraries, and GPU telemetry needs the
normal installed graphics driver.

| Platform | Default CPU temperature / power | Default GPU telemetry |
| --- | --- | --- |
| Windows x64 / ARM64 | Unavailable through the native APIs used here | NVIDIA driver NVML: temperature, watts, clocks, utilization and limits where supported |
| Linux x64 / ARM64 | Kernel hwmon temperatures/power and readable RAPL energy counters | AMD/nouveau hwmon temperature and exposed power channels; other kernel-exposed sensors |
| Apple Silicon / Intel macOS | Unavailable in the default configuration | Unavailable in the default configuration |

Recording, hardware surveys, imports, reports and comparisons work on all targets.
Missing sensor readings remain unknown. The CPU benchmark retains its missing-temperature
guard; using it without a sensor requires an explicit `--allow-unmonitored`.
No fallback installs, downloads, elevation, or network requests run automatically.

Hardware and process information uses Windows APIs/registry, Linux procfs/sysfs,
and macOS sysctl plus built-in `ps`, `top`, and `system_profiler`. macOS CPU
usage sampling takes about one second, which can lengthen capture intervals.
Physical core count is unknown (0 in JSON) when topology is not exposed.
On Windows systems with more than 64 logical CPUs, aggregate CPU usage is left
unknown because the API used reports only one processor group.

Optional integrations from earlier versions remain available, **off by default**:
set `THERMAL_EXTERNAL_PROVIDERS=1` to allow LibreHardwareMonitor WMI via PowerShell
on Windows, macmon on Apple Silicon, nvidia-smi as a GPU fallback, and lspci for
Linux GPU names. Install/configure only the integration you choose. These are
optional extensions and are not included with the binary. In particular, Linux
NVIDIA telemetry may need this opt-in when the driver exposes no usable hwmon
sensors. Native Windows NVIDIA telemetry works without nvidia-smi.

The direct NVIDIA binding follows the
[official NVML API](https://docs.nvidia.com/deploy/nvml-api/group__nvmlDeviceQueries.html).
Unsupported driver fields remain unknown; overlapping power channels are never summed.

The top-app list is a brief sample before capture, with 100% meaning one logical CPU.
GPU process attribution and continuous process history are not implemented.
CPU and GPU fan RPM/control are not implemented; fan cleaning is an intervention label.

## Import existing thermal CSVs

Supports the column names from the original GPU test logs:

```csv
sec,temp_C,power_W,util_pct,clock_MHz,pstate,hw_thermal,sw_thermal
0,61,6.97,5,210,P8,Not Active,Not Active
5,84,174.80,100,2490,P0,Not Active,Not Active
```

```sh
thermal import --stage baseline --device laptop-gpu --out before.json before.csv
thermal import --stage fans-cleaned --device laptop-gpu --out after.json after.csv
thermal compare before.json after.json
```

Only `sec` and `temp_C` are required. Use the same `--device` for the same GPU.
Imported metadata is explicitly marked unverified; timestamps are import times.
Optional power/utilization/clock/throttling values retain missingness.
`pstate` is accepted in input but is not currently retained.

## Saved data and automation

Runs are JSON with schema version 1. Default storage:

- Windows: `%AppData%/thermal/runs`
- macOS: `~/Library/Application Support/thermal/runs`
- Linux: `$XDG_CONFIG_HOME/thermal/runs` or `~/.config/thermal/runs`

Set `THERMAL_DATA_DIR` to choose another default, or use `--out`.
Existing files are never overwritten. `history` lists the default directory only.
Files contain hostname, sensor identifiers, measurements, notes and sampled process
names/PIDs. Data stays local; inspect files before sharing them.

`doctor --json`, `record --json`, `benchmark --json`, and
`compare BEFORE AFTER --json` emit machine-readable JSON to stdout.
Progress and saved-file paths go to stderr.

## Development

Go 1.23+ is the only build requirement. Run from the repository root:

```sh
go run ./scripts/dist check-deps
go test ./...
go vet ./...
go run ./scripts/dist build --local
```

Run native executable smoke tests after rebuilding:

```sh
THERMAL_TEST_NATIVE=1 go test ./scripts/smoke -count=1
sh scripts/install.test.sh
```

On PowerShell, set `$env:THERMAL_TEST_NATIVE = '1'`, run `go test ./scripts/smoke -count=1`, then run `./scripts/install.test.ps1`.

The dependency check rejects external Go modules and non-standard imports across all six targets. Builds disable cgo and module downloads. Local binaries are written to `dist/<os>-<arch>/thermal` (`thermal.exe` on Windows), using `windows`, `linux`, or `darwin` and `amd64` or `arm64`.

To build all targets and prepare release archives, installers, and checksums:

```sh
go run ./scripts/dist build
go run ./scripts/dist package
```

Assets are written to a fresh `release/v<version>/` directory; packaging refuses to overwrite it. The version comes from `internal/thermal/model.go`. `GITHUB_REPOSITORY` selects the release repository and defaults to `preacherxp/thermal` for local builds. Installers in release assets are pinned to that version; when testing a source installer, set `THERMAL_VERSION` explicitly.

CI tests on Windows, macOS, and Linux and uploads the release assets. Pushing a `v<version>` tag publishes those assets to GitHub Releases after the checks pass. The tag must match the binary version. Normal branch builds do not publish.

Tests cover native sensors, reports, benchmark safety, comparisons, storage, archive contents, and installer success/failure paths. Benchmark unit tests briefly load one CPU worker and use fake GPU sessions. Native smoke tests use passive recording. Set `THERMAL_TEST_BENCHMARK=1` and run `go test ./cmd/thermal -run TestNativeDefaultBenchmark -count=1` after building to check the real default benchmark and PNG output.

Layout: `cmd/thermal` (commands), `internal/thermal` (sensors, capture, reports, storage), `scripts/dist` (Go build and release tooling), `scripts/smoke` (native CLI tests), `install.sh` and `install.ps1` (release installers).

## Detailed power readings (0.2)

`thermal doctor` displays device panels with measured watts and power limits in
separate groups. Recordings and reports retain last/minimum/mean/peak watts for each
channel; comparisons show changes in mean and peak watts. The mean is over available
samples, not a time-weighted energy measurement. Missing values remain unknown.

| Provider | Power details |
| --- | --- |
| NVIDIA | Board draw, instantaneous board draw, one-second average, requested/enforced/default/minimum/maximum limits where supported |
| LibreHardwareMonitor (Windows, opt-in) | All exposed Power sensors, such as CPU package, CPU cores, uncore, memory, and GPU domains |
| Linux hwmon | Available power input/average channels, converted from microwatts to watts |
| Linux RAPL | CPU package/subdomain interval-average watts derived from energy counter differences; available power constraints |
| macmon (Apple Silicon, opt-in) | CPU, GPU, GPU SRAM, RAM, neural engine, combined domains, and system estimate where exposed |

CPU package/core and GPU board/subdomain measurements **overlap**. They are never
summed into an invented total. Power limits describe constraints, not consumption.
The optional Windows LHM adapter uses named whole-device channels as primary power.
For Linux hwmon, the primary power channel is explicitly labelled power1.
New JSON data remains schema 1 with optional `power_readings` fields; old runs load
normally. A detailed channel is compared only when IDs and measurement modes match.

RAPL needs readable `energy_uj` counters. Some Linux systems restrict access to root.
Thermal does not elevate itself or change permissions. It primes two readings,
handles a single counter wrap using `max_energy_range_uj`, and reports unknown after
missing data or long gaps. Counter resets cannot always be distinguished from wraps.
RAPL is an interval average, not an instantaneous reading.
See the [kernel powercap documentation](https://www.kernel.org/doc/html/latest/power/powercap/powercap.html).

NVIDIA's averaged/instantaneous readings depend on GPU and driver support.
See [NVIDIA power readings](https://docs.nvidia.com/deploy/nvidia-smi/index.html).
Native Windows NVML loads from System32 or the Windows DriverStore. Optional
nvidia-smi discovery checks PATH and standard Windows driver locations. With
external providers enabled, set `THERMAL_NVIDIA_SMI` for an explicit executable path.

Colors are enabled for interactive terminals and disabled in redirected output.
Set `NO_COLOR=1` for plain output. JSON output contains no terminal decoration.
Interactive recording uses a compact progress line with CPU/GPU temperatures and
the selected primary power channel, rather than printing a long sensor line each time.

## Pre-run survey (0.3)

The one-command shortcut (`thermal` or `thermal [options]`) skips the survey by
default. Explicit `record` or `benchmark` commands identify the CPU, GPU, machine model,
core/thread counts and available RAM, then presents three short sections:

1. **Expected GPU watts:** OEM-rated maximum where an exact model reference exists,
   alongside the driver's current enforced and configurable maximum limits.
2. **Thermal material:** paste, liquid metal, phase-change material, pad, other or
   unknown. CPU and discrete GPU answers are separate. Factory references never
   automatically assert what is currently installed.
3. **Expected scores:** benchmark-specific, profile-specific reference results,
   with sources saved in JSON. Additional targets can be entered manually.

On a real terminal the survey asks for your answers. Press Enter to keep a value.
Type `factory` for a material only when it is unchanged from the factory reference.
Closing input or pressing Ctrl+C during the survey exits before starting a load.
With redirected input, the default `--survey auto` prints the detected summary and
keeps unknown answers. `--json` also avoids automatic prompts.
Use `--survey ask` to explicitly answer from a terminal or a pipe, or
`--survey skip` to omit the step. `ask` cannot be combined with `--json`.

Preview without a benchmark:

```sh
thermal survey
thermal survey --json
thermal survey --survey ask
```

Supply known values for automation:

```sh
thermal record --duration 30s --workload my-game --profile performance --power ac --gpu-watts 175 --cpu-material factory --gpu-material phase-change --out run.json --json

thermal benchmark --survey skip --duration 30s --out run.json
```

`--gpu-watts` and `--gpu-material` address the first discrete GPU in the survey.
When discrete graphics are present, known integrated Intel/Apple graphics are not
treated as another independently pasted GPU. GPU inventory on Windows prefers
NVIDIA's physical-device list; the native display API is used when it is unavailable.

For an additional expected score, provide both `--expected-benchmark` and
`--expected-score`, optionally `--score-source`. Include the benchmark version,
component and preset in the name. These targets are recorded as context; the app
does not run third-party benchmarks or grade its CPU workload against their scores.

The initial offline reference catalog covers **Lenovo Legion Pro 7 16IRX8H / 82WQ,
Core i9-13900HX, RTX 4080 Laptop GPU**:

- OEM GPU maximum: 175 W, from
  [Lenovo PSREF](https://psref.lenovo.com/syspool/Sys/PDF/datasheet/Legion_Pro_7_16IRX8_datasheet_EN.pdf).
- Factory CPU liquid metal and GPU PTM795X series phase-change material, from
  [Lenovo's service material table](https://seservice.lenovo.com/static_file/tape/tape.html).
- Published single-review-unit results: Time Spy **graphics** 18,822 performance /
  11,668 balanced; Cinebench R23 **multi-core** 27,237 performance / 21,080 balanced.
  Source: [the比較's measured review](https://thehikaku.net/pc/lenovo/23Legion-Pro-7i-Gen8.html).

Those scores are illustrative reference measurements, **not a population average,
guaranteed target, or pass/fail threshold**. Benchmark settings, memory, drivers and
power profiles affect results. Thermal's own CPU SHA-256 hashes/s has no validated
model reference database and cannot be converted to these scores. The built-in
CPU-only load is not expected to bring a GPU to its rated maximum watts.
The built-in GPU integer workload also differs from graphics benchmarks and does
not guarantee maximum board power.

Other machines still get hardware detection and manual survey inputs; unverified
OEM wattages/materials/scores remain unknown. References are bundled and dated
2026-09-06. The application performs no web searches or hardware uploads at runtime.
Survey answers, hardware, sources and score provenance are saved under
`pre_run_survey` in each run. Existing schema-1 files remain readable.
