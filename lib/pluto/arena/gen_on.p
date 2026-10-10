//pu:build gencheck

package arena

// genCheck selects arena epochs (epoch.p): built with -gencheck, a Chunked
// tags the pointers it hands out with an epoch that its Reset, Release and
// Drop end.
const genCheck = true
