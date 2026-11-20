# Still frames for the README, recorded in the same throwaway world as the demo.
#
# Never run this tape directly — `just screenshots` hands it to scripts/demo.mjs,
# which builds ccu, seeds the stacks and starts the fake registry first.

# vhs insists on a recording; the stills are the point, so it goes to /tmp.
Output "/tmp/ccu-screenshots.gif"

Require ccu

Set Shell "bash"
Set FontSize 15
Set Width 1500
Set Height 560
Set Padding 26
Set Margin 18
Set MarginFill "#1a1420"
Set BorderRadius 10
Set Theme "Catppuccin Mocha"
Set TypingSpeed 20ms

Hide
Type "source $DEMO_SETUP" Enter
Type "clear" Enter
Show

# Everything selected, so the hero shot shows the bar counting what A would write.
Type "ccu" Enter
Wait+Screen /update\(s\) found in/
Sleep 1000ms
Ctrl+A
Wait+Screen /apply 9/
Sleep 800ms
Screenshot assets/tui.png

# traefik is the last row; stepping its target back is the choice the TUI exists for.
PageDown
Sleep 800ms
Right
Wait+Screen /target/
Type "-"
Wait+Screen /v2.11.4/
Sleep 1200ms
Screenshot assets/tui-details.png
# vhs drops a Screenshot that is the tape's last command.
Sleep 500ms
