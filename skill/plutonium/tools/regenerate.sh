#!/bin/sh
# Regenerates the generated references of the Plutonium skill from the
# sources: references/stdlib*.md from lib/pluto (through pluto doc),
# references/diagnostics.md from selfhost/internal/diag and pluto explain.
# Run it from anywhere after the library or the diagnostics catalog
# changes, then run `go test ./tools/skillcheck`. It needs a pluto
# compiler: $PLUTO if set, otherwise pluto on PATH.
set -eu
cd "$(dirname "$0")/../../.."
unset GOROOT
pluto="${PLUTO:-pluto}"
go run ./skill/plutonium/tools/genstdlib -pluto "$pluto"
go run ./skill/plutonium/tools/gendiag -pluto "$pluto"
echo "regenerated skill/plutonium/references/stdlib*.md and diagnostics.md" >&2
