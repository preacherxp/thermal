Output examples/demo.gif
Output examples/demo.mp4

Require thermal
Set Shell bash
Set FontFamily "Menlo"
Set FontSize 20
Set Width 1200
Set Height 860
Set Padding 20
Set Theme "TokyoNightStorm"
Set WindowBar Colorful
Set Framerate 15
Set TypingSpeed 45ms
Set CursorBlink false
Env THERMAL_EXTERNAL_PROVIDERS "0"

Hide
Type `cd "$(mktemp -d)" && unset NO_COLOR && clear`
Enter
Wait />$/
Show

Type "thermal --version"
Enter
Sleep 1s
Type "thermal --target cpu --duration 8s --workers 2 --out run.json"
Sleep 500ms
Enter
Sleep 1s
Wait@60s />$/
Sleep 6s
