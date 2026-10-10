//pu:build allocsites

package rt

// TrackSites reports whether allocations publish their call site through
// AllocSite. The allocsites build tag turns it on; "pluto build
// --alloc=debug" sets the tag. Without it the bookkeeping compiles away.
const TrackSites = true
