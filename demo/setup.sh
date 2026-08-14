# Sourced by the tape inside the recording, before anything is shown.
#
# The driver (scripts/demo.mjs) has already built ccu, seeded the throwaway
# stacks and started the fake registry; it passes their locations in the
# environment. Everything this file does is make the shell on screen look like
# a plain shell sitting in a plain directory — and nothing on screen belongs to
# whoever is recording.
set -euo pipefail

# The real prompt carries $USER and the hostname. That is a leak, and it is the
# easiest one to miss, because it still just looks like a prompt.
PS1='\[\e[38;5;183m\]~/stacks\[\e[0m\] $ '

export HOME="$CCU_DEMO_HOME"          # ccu writes its config here, never the real one
export PATH="$CCU_DEMO_BIN:$PATH"
export CCU_NO_UPDATE_CHECK=1          # a "new version available" line dates the GIF forever
export PAGER=cat GIT_PAGER=cat
export CLICOLOR_FORCE=1
# CCU_REGISTRY_HOST is exported by the driver: every tag on screen is served by
# demo/fake-registry.mjs, so the recording is reproducible and offline.

cd "$CCU_DEMO_STACKS"
