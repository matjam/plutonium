//pu:build kernel

package rt

// In kernel mode a runtime Lock disables interrupts on its processor while
// it is held (preempt_kernel_amd64.s), so that an interrupt handler may take
// it: a non-blocking channel operation in a handler cannot deadlock against
// the code it interrupted.

func irqSave() uintptr
func irqRestore(flags uintptr)

func lockAcquire(l *Lock) {
	flags := irqSave()
	for !cas32(&l.key, 0, 1) {
		irqRestore(flags)
		for load32(&l.key) != 0 {
			pause()
		}
		flags = irqSave()
	}
	l.flags = flags
}

func lockTry(l *Lock) bool {
	flags := irqSave()
	if cas32(&l.key, 0, 1) {
		l.flags = flags
		return true
	}
	irqRestore(flags)
	return false
}

func lockRelease(l *Lock) {
	flags := l.flags
	store32(&l.key, 0)
	irqRestore(flags)
}
