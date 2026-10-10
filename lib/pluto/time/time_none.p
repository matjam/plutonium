//pu:build none

package time

// fireTime returns the time a timer sends when it fires. Freestanding
// builds have no wall clock: the Time carries only the monotonic reading
// mono of the scheduler's clock (rt.Now), so Sub, Before and After compare
// such times correctly, while their date is the zero Time's.
func fireTime(mono int64) Time {
	return Time{hasMono: true, mono: mono}
}
