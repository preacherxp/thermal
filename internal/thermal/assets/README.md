# Report font

`Go-Regular.ttf` comes from the Go project's font collection:
https://github.com/golang/image/tree/master/font/gofont/ttfs

The upstream font license is in `LICENSE`. The binary distribution includes it
through `scripts/notices.cjs`.

`font.png` and `font.json` contain pre-rendered glyphs and metrics at 18, 24, 36,
and 52 pixels. Regenerate them on Windows with `./scripts/font-atlas.ps1`.
Only the atlas and metrics are embedded in the Go binary. Normal builds and
exports do not use the TTF, PowerShell, System.Drawing, or system fonts.
