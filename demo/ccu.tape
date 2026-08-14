# The ccu demo: what is outdated, which version you actually want, applied.
#
# Never run this tape directly — `just demo` (node scripts/demo.mjs) records it.
# The driver builds ccu, seeds invented Compose stacks under $TMPDIR, points HOME
# at a throwaway directory, and starts demo/fake-registry.mjs, which serves every
# tag you see here. No real registry, no real config, no real stacks: the
# recording is reproducible and offline, and exposes nothing of the recorder's.

Output assets/demo.gif

Require ccu

Set Shell "bash"
Set FontSize 15
Set Width 1500
Set Height 520
Set Padding 26
Set Margin 18
Set MarginFill "#1a1420"
Set BorderRadius 10
Set Theme "Catppuccin Mocha"
Set TypingSpeed 55ms
Set Framerate 24

Hide
Type "source $DEMO_SETUP" Enter
Type "clear" Enter
Show

# ---- one of four stacks, pinned where it was pinned months ago ----
# Opening on the file makes the closing frame a before/after rather than a claim.
Type "cat proxy/compose.yaml" Sleep 400ms Enter
Wait+Screen /traefik/
Sleep 3200ms

# ---- every stack below here, checked against its registry ----
# Rows stream in per file while the scan is still running; the status line
# settles on the totals, which is the first thing worth reading.
Hide
Type "clear" Enter
Show
Type "ccu" Sleep 800ms Enter
Wait+Screen /update\(s\) found in/
Sleep 4500ms

# ---- the row you would not have taken blindly ----
# `pgdn` clamps to the last row, which is traefik — a deterministic anchor,
# unlike the cursor's resting place after a concurrent scan. traefik is the
# story: on v2.9.3 with a v3 out, the default offer is a major bump.
PageDown
Sleep 1500ms
Right
Wait+Screen /target/
Sleep 2500ms

# `-` steps the target back one level: major v3.2.0 -> the newest 2.11.x instead.
# Nothing about the row changes except which release it will write.
Type "-"
Wait+Screen /v2.11.4/
Sleep 4000ms
Left
Sleep 1500ms

# ---- take the rest as offered, then write ----
# The bar counts what is selected, so `[ apply 9 ]` is the confirmation that
# ctrl+a caught every row before A writes anything.
Ctrl+A
Sleep 2200ms
Type "A"
Wait+Screen /docker compose up -d/
Sleep 4000ms

# No docker is involved in a recording, and the answer is the honest one anyway:
# ccu asks, it never restarts anything by itself.
Type "n"
Sleep 1800ms

# ---- and it is on disk ----
Hide
Type "clear" Enter
Show
Sleep 600ms
Type "cat proxy/compose.yaml" Sleep 400ms Enter
Wait+Screen /v2.11.4/
Sleep 3600ms

# The .ccu file next to it is the backup ccu wrote before touching anything.
Type "ls proxy" Sleep 400ms Enter
Wait+Screen /compose.yaml.ccu/
Sleep 3500ms
