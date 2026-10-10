//pu:build none && !kernel

package rt

// A freestanding program outside kernel mode may not be allowed to mask
// interrupts, so a runtime Lock only spins.

func lockAcquire(l *Lock) {
	for !cas32(&l.key, 0, 1) {
		for load32(&l.key) != 0 {
			pause()
		}
	}
}

func lockTry(l *Lock) bool { return cas32(&l.key, 0, 1) }

func lockRelease(l *Lock) { store32(&l.key, 0) }
