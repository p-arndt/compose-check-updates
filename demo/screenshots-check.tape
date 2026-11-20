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
Set Height 340
Set Padding 26
Set Margin 18
Set MarginFill "#1a1420"
Set BorderRadius 10
Set Theme "Catppuccin Mocha"
Set TypingSpeed 20ms

Hide
Type "source $DEMO_SETUP" Enter
Type "clear" Enter
# The cache summary on stderr is true but noise in a still.
Type "export CCU_NO_CACHE=1" Enter
Type "clear" Enter
Show

Type "ccu check -f" Enter
# Rows arrive in the order registries answer, so there is no last line to wait
# for; the fake registry is local and done well within this.
Wait+Screen /INFO/
Sleep 4000ms
Screenshot assets/check.png
# vhs drops a Screenshot that is the tape's last command.
Sleep 500ms
