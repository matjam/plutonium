// The benchmark suite is its own module so that the repository's root module
// (go build ./..., go vet ./...) ignores it. The per-benchmark main.go files are
// built one at a time by the harness, never as part of this module.
module github.com/matjam/plutonium/bench

go 1.26
