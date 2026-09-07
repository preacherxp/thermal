# Run Thermal

[Install with curl or download a prebuilt GitHub release](README.md#install). After installing and adding the executable's directory to PATH, run:

```sh
thermal
```

This benchmarks the CPU for 30 seconds, then the GPU for 30 seconds, and saves JSON, PDF, and PNG results without survey prompts. The PDF adds a minimal findings overview, separate CPU/GPU detail pages, thermal charts, and a suggested next step. The PNG contains separate scores, thermal timelines and test statuses. Saved paths appear in the terminal. Press Ctrl+C to stop early and save partial results.

The GPU benchmark uses the system OpenCL framework on macOS and an installed OpenCL GPU driver on Windows. Each test requires a readable target temperature; missing monitoring is shown as a refused test. macOS reads native AppleSMC temperatures without sudo or extra tools: CPU die temperatures where available, and GPU temperatures on Apple Silicon. CPU/GPU wattage on Apple Silicon is collected automatically through IOReport and shown in the console, JSON, PDF, and PNG as interval-average energy-model estimates. Intel/discrete GPU monitoring still requires an identifiable sensor provider. Windows CPU temperatures require an optional provider. Explicitly choose `thermal --allow-unmonitored` only when bounded tests without temperature readings are intended. Any available temperature limits still apply.

Interactive runs open the live dashboard by default. Use `thermal --no-tui` for minimal output or `thermal --verbose` for full console tables. To inspect all details later: `thermal report run.json`. Unsupported terminals and redirected output automatically use the minimal fallback. In the dashboard, `q`, Esc, or Ctrl+C stops safely and saves partial results.

To change the duration per test: `thermal --duration 60s`.
For passive monitoring: `thermal record --survey skip --duration 30s`.

## Downloaded executable on Windows (PowerShell)

Extract the Windows release ZIP, open a terminal in that folder, and run:

```powershell
.\thermal.exe
```

Optional duration and output path:

```powershell
.\thermal.exe --duration 60s --out run.json
```

## More commands

Check which sensors are available:

```powershell
.\thermal.exe doctor
```

Try a sample PDF report without running a benchmark:

```powershell
.\thermal.exe demo --pdf demo.pdf --png demo.png
Invoke-Item .\demo.pdf
```

Record temperatures for 30 seconds (no built-in CPU load):

```powershell
.\thermal.exe record --duration 30s --out before.json
Invoke-Item .\before.png
```

This saves **before.json** (measurements), **before.pdf** (findings), and **before.png** (visual summary).
For a game or GPU workload, start it first and add `--workload my-game`.
The survey may ask about your hardware; press Enter to keep an unknown/default answer.
Add `--survey skip` for a recording without survey prompts.

## Compare a change

Keep the same workload, duration, power source, and room conditions. After changing one thing, record again:

```powershell
.\thermal.exe record --duration 30s --stage apps-closed --out after.json
.\thermal.exe compare before.json after.json --pdf comparison.pdf --png comparison.png
Invoke-Item .\comparison.png
```

Use `apps-closed` only if that is what you changed. Run `thermal stages` using the executable path above to see other labels.

To export a previously saved run:

```powershell
.\thermal.exe report after.json --pdf report.pdf --png report.png
```

Files are never overwritten. Choose a new filename when repeating these examples.
Use `--pdf custom.pdf` or `--png custom.png` to choose export paths. Add `--no-pdf` or `--no-png` to skip either export during `record`, `benchmark`, or `import`; use both to save only JSON.

## Test only one component

This runs a CPU workload for 30 seconds. It requires a readable CPU temperature and stops at the configured temperature limit. Use `doctor` first.

```powershell
.\thermal.exe --target cpu --duration 30s --out cpu-test.json
.\thermal.exe --target gpu --duration 30s --out gpu-test.json
```

The built-in GPU test measures integer compute throughput, not gaming FPS. Use `record` alongside a game or renderer for that workload's thermals. Ctrl+C stops a recording/benchmark and saves partial results. Failed or unavailable phases are retained in the report; partial suites return exit code 1.

## macOS / Linux / Windows ARM

Download the matching architecture from the [release asset table](README.md#2-download-a-prebuilt-github-release). Windows ARM64 uses the same `.\thermal.exe` commands above. On macOS/Linux, run from the extracted folder:

```sh
./thermal demo --pdf demo.pdf --png demo.png
```

Use `chmod +x thermal` if your extraction tool dropped executable permissions. Open the report with `open demo.png` on macOS, or `xdg-open demo.png` on Linux.

## Build from source (optional)

With Go 1.23 or newer installed:

```sh
go run ./cmd/thermal demo --pdf demo.pdf --png demo.png
```

`go run ./scripts/dist build --local` updates your platform's executable in `dist/<os>-<arch>`. `go run ./scripts/dist build` builds all six targets. The first build downloads the pinned Go TUI modules; no font tools or cgo are needed. Distributed executables remain standalone.

See [README.md](README.md) for sensor setup and all options.
