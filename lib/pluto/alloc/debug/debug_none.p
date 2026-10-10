//pu:build none

package debug

import "pluto/alloc"

// Freestanding builds have no default page provider: pass the kernel's in
// Options.Pages. Nothing is hooked up either; the kernel can call
// DescribeFault from its page fault handler, before rt.PanicFault, and
// ReportLeaks when it likes.

func defaultPages() alloc.PageProvider { return nil }

func register(d *Allocator) {}
