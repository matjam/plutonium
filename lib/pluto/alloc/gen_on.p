//pu:build gencheck

package alloc

// genCheck selects the generational-reference prototype: the slab tags the
// pointers it hands out with the slot's generation, and the compiler (built
// with -gencheck) checks the tag on every access.
const genCheck = true
