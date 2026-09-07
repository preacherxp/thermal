# Thermal CLI: agent usage

## Installation and default benchmark

Install with curl or download a prebuilt GitHub release; see [README.md](README.md#install). The standalone executable needs no language runtime.

Run both benchmarks and save PDF findings and PNG results with one command:

```sh
thermal
```

This benchmarks the CPU for 30 seconds, then the GPU for 30 seconds, without survey prompts. It saves JSON measurements, a PDF findings report, and a PNG with separate CPU/GPU scores, thermal timelines, and statuses. The GPU compute backend supports macOS through its system OpenCL framework and Windows with an installed OpenCL GPU driver. Unsupported or refused tests are clearly marked; no score is fabricated.

Each target requires its own readable temperature. macOS reads native AppleSMC CPU die temperatures and Apple Silicon GPU temperatures. Apple Silicon CPU/GPU wattage is read natively through IOReport; values are interval-average energy-model estimates, not wall power. Intel/discrete GPU sensor identity is not inferred. Windows CPU temperature is unavailable through the native APIs used here, so its CPU phase is refused unless a provider is configured or the user explicitly chooses `--allow-unmonitored`. The monitored GPU phase can still run. A partial suite returns exit code 1 and saves its results.

For passive monitoring, use `thermal record --survey skip --duration 30s`.

Below, `thermal` means the installed executable on PATH. From an extracted release, use `./thermal` on Linux/macOS or `.\thermal.exe` on Windows. Local Go builds use `dist/<os>-<arch>/thermal` (`thermal.exe` on Windows), with OS names `windows`, `linux`, `darwin` and architectures `amd64`, `arm64`. Linux/macOS binaries may need `chmod +x` if extracted without executable permissions.

## Common tasks

| Task | Command |
| --- | --- |
| CPU + GPU benchmarks, PDF, and PNG | `thermal` |
| JSON only | `thermal record --survey skip --json --no-pdf --no-png` |
| Choose duration and output | `thermal record --survey skip --duration 60s --out run.json` |
| Record an already running game | `thermal record --survey skip --workload my-game --duration 120s` |
| Inspect sensors without recording | `thermal doctor --json` |
| Inspect hardware without prompts | `thermal survey --json` |
| Run only the CPU benchmark | `thermal --target cpu` |
| Run only the GPU benchmark | `thermal --target gpu` |
| Compare saved runs | `thermal compare before.json after.json --json --png comparison.png` |
| Export a saved run | `thermal report run.json --pdf report.pdf --png report.png` |
| List default saved runs | `thermal history` |
| Synthetic example, no load | `thermal demo --pdf demo.pdf --png demo.png` |
| See all benchmark options | `thermal benchmark --help` |

## Rules for automation

- Bare `thermal` and benchmark flags without a subcommand default to
  `benchmark --target both --survey skip`. `--duration` applies to each target.
  Explicit `record` and `benchmark` retain their interactive
  survey default; use `--json` or `--survey skip` to avoid prompts. To supply survey
  answers through the shortcut, add `--survey auto --json` and the survey inputs.
- Interactive captures use the Bubble Tea/Lip Gloss dashboard by default.
  `--no-tui` selects minimal output; `--verbose` restores full tables and recommendations.
  Pipes, `--json`, unsupported terminals, `NO_COLOR`, and `TERM=dumb` use the fallback.
  The dashboard enables Windows virtual terminal processing when supported and
  restores terminal state on exit. It never starts an extra workload after a UI failure.
  `q`, Esc, and Ctrl+C cancel the workload and wait for partial reports to be saved.
  Large scores use SI prefixes (k = thousand, M = million, G = billion, T = trillion).
  `thermal report run.json` shows full saved details without starting load.
- JSON stdout is supported by captures, `doctor`, `survey`, and `compare`.
  Read saved JSON for `report` or `import`. Keep stderr separate from JSON stdout.
- `record`, `benchmark`, and `import` save JSON, PDF, and PNG automatically.
  `--pdf FILE.pdf` and `--png FILE.png` change export paths; `--no-pdf` and
  `--no-png` disable them independently. Do not combine a path flag with its disable flag.
  `report`, `compare`, and `demo` export only when `--pdf` or `--png` is supplied.
  PDFs contain a findings overview, target-specific measurements, charts, and next
  steps. Missing readings and incomplete tests remain explicit; thresholds are
  general review cues, not device-specific diagnoses. Fonts are embedded.
- Files are never overwritten. Use fresh names. `--out` saves independently of
  stdout; do not redirect stdout to the same file. If a PDF or PNG export fails after JSON
  is saved, regenerate with `report` instead of repeating the measurement.
- Default storage: `%APPDATA%/thermal/runs` on Windows,
  `~/Library/Application Support/thermal/runs` on macOS, and
  `$XDG_CONFIG_HOME/thermal/runs` or `~/.config/thermal/runs` on Linux.
  `THERMAL_DATA_DIR` overrides it. `history` searches only that directory.
- Exit code 1 can accompany saved partial/refused/stopped results. Inspect
  `status`, `warnings`, and stderr. Ctrl+C during capture saves partial results.
- Combined JSON uses `workload: cpu-gpu-suite-v1` and a `phases` array containing
  separate CPU and GPU runs. Read samples, warnings and scores inside each phase;
  the suite does not combine their sensor timelines. `report` and `compare`
  understand these suites. GPU scores are verified integer iterations/second;
  CPU scores are SHA-256 hashes/second. Neither converts to gaming FPS or other
  benchmark scores.
- The CPU benchmark requires readable CPU temperature and defaults to a 90 C stop
  limit and at most four workers. Do not add `--allow-unmonitored` merely to make
  a refused test pass. Use it only when unmonitored CPU load is explicitly intended.
- For game-specific GPU tests, run the external workload separately and use recording.
  `--profile`, `--power`, and `--stage` describe actual conditions/actions; they do
  not change settings or hardware. Leave unknown facts unknown.
- Compare the same device/workload under comparable conditions. Valid stages are
  `baseline`, `apps-closed`, `profile-changed`, `fans-cleaned`, `repasted`, `specialist`.
  Comparisons use the last 25% of captured time, with deltas after minus before.
  Review warnings; temperatures alone do not prove a cooling fault or causality.
- Missing/null readings mean unknown, not zero. Power limits are not consumption;
  overlapping power channels must not be summed. Demo readings are synthetic.
- Native CPU temperatures are unavailable on Windows with the default provider.
  macOS uses read-only AppleSMC access and native Apple Silicon IOReport power
  counters; unavailable temperature sensors still refuse load. Missing energy
  counters and invalid/reset sampling intervals produce unknown watts, not zero.
  Optional integrations require `THERMAL_EXTERNAL_PROVIDERS=1` and
  separate setup; consult [README.md](README.md). No providers install automatically.

CSV import: `thermal import --device same-gpu --out run.json input.csv`.
Put flags before the CSV path. Required columns: `sec,temp_C`. Use the same
`--device` across before/after imports; imported conditions are unverified.

See [RUN.md](RUN.md) for quick examples and [README.md](README.md) for full options.

## Development

Go 1.23+; pinned external Go TUI libraries are permitted, no cgo. Commands: `cmd/thermal`; measurement/report implementation: `internal/thermal`; dashboard: `internal/tui`; distribution tooling: `scripts/dist`. Preserve unknown readings, non-overwriting saves, and clean JSON stdout.

Download modules with `go mod download`. Dependency versions are pinned in `go.mod`/`go.sum`; update `THIRD_PARTY_LICENSES.txt` from upstream module licenses when dependencies change. Release archives bundle these notices; `check-deps` verifies checksums, license coverage, and all six platforms.

After code changes, run `go test ./...`, `go vet ./...`, and `go run ./scripts/dist check-deps`. Rebuild with `go run ./scripts/dist build --local`, then set `THERMAL_TEST_NATIVE=1` and run `go test ./scripts/smoke -count=1` for CLI/distribution changes. Run `sh scripts/install.test.sh` on Linux/macOS and `./scripts/install.test.ps1` on Windows for installer changes.

To verify the actual default CPU/GPU workflow after building, set
`THERMAL_TEST_BENCHMARK=1` and run
`go test ./cmd/thermal -run TestNativeDefaultBenchmark -count=1`.
This starts monitored load where supported; ordinary benchmark unit tests use
fake GPU sessions and short CPU workloads.

`go run ./scripts/dist build` builds all six targets. `go run ./scripts/dist package` writes versioned release archives, pinned installers, and checksums to a fresh directory under `release/`. CI publishes these assets on version tags after tests pass. Use executable help and parser inspection to verify documentation-only changes.
