# Development-only: regenerate the embedded atlas on Windows. Normal builds use
# the checked-in PNG/JSON; no font libraries or system fonts are needed at runtime.
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing
$assetDir = Join-Path $PSScriptRoot '../internal/thermal/assets'
$fonts = New-Object System.Drawing.Text.PrivateFontCollection
$fonts.AddFontFile((Join-Path $assetDir 'Go-Regular.ttf'))
$bitmap = New-Object System.Drawing.Bitmap 1536, 1024
$graphics = [System.Drawing.Graphics]::FromImage($bitmap)
$graphics.Clear([System.Drawing.Color]::Transparent)
$graphics.TextRenderingHint = [System.Drawing.Text.TextRenderingHint]::AntiAliasGridFit
$format = [System.Drawing.StringFormat]::GenericTypographic.Clone()
$format.FormatFlags = $format.FormatFlags -bor [System.Drawing.StringFormatFlags]::MeasureTrailingSpaces
$glyphs = @{}
$x = 0; $y = 0; $rowHeight = 0
foreach ($size in @(18, 24, 36, 52)) {
    $font = New-Object System.Drawing.Font $fonts.Families[0], $size, ([System.Drawing.FontStyle]::Regular), ([System.Drawing.GraphicsUnit]::Pixel)
    foreach ($code in (32..126 + @(176, 183, 8594))) {
        $character = [string][char]$code
        $advance = [int][Math]::Ceiling($graphics.MeasureString($character, $font, 1000, $format).Width)
        $width = $advance + 6
        $height = [int][Math]::Ceiling($font.GetHeight($graphics)) + 6
        if ($x + $width -gt $bitmap.Width) { $x = 0; $y += $rowHeight; $rowHeight = 0 }
        $graphics.DrawString($character, $font, [System.Drawing.Brushes]::White, [single]($x + 2), [single]($y + 2), $format)
        $glyphs["${size}:$code"] = @{X=$x; Y=$y; W=$width; H=$height; Advance=$advance}
        $x += $width
        $rowHeight = [Math]::Max($rowHeight, $height)
    }
    $font.Dispose()
}
if ($y + $rowHeight -gt $bitmap.Height) { throw 'Atlas overflow' }
$bitmap.Save((Join-Path $assetDir 'font.png'), [System.Drawing.Imaging.ImageFormat]::Png)
$glyphs | ConvertTo-Json -Compress | Set-Content -Encoding utf8 (Join-Path $assetDir 'font.json')
$format.Dispose(); $graphics.Dispose(); $bitmap.Dispose(); $fonts.Dispose()
