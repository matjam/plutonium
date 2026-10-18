#!/bin/sh
# Regenerates the generated references of the Plutonium skill from the
# sources: references/stdlib*.md from lib/pluto, references/diagnostics.md
# from internal/diag. Run it from anywhere after the library or the
# diagnostics catalog changes, then run `go test ./tools/skillcheck`.
set -eu
cd "$(dirname "$0")/../../.."
unset GOROOT
go run ./skill/plutonium/tools/genstdlib
go run ./skill/plutonium/tools/gendiag
echo "regenerated skill/plutonium/references/stdlib*.md and diagnostics.md" >&2
