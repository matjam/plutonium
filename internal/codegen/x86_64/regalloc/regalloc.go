package regalloc

import (
	"github.com/matjam/plutonium/internal/codegen/x86_64/encoder"
	"github.com/matjam/plutonium/internal/ir"
)

// Allocation maps IR value IDs to physical locations.
type Allocation struct {
	Reg       map[int]encoder.Reg // value ID → physical register
	Spill     map[int]int         // value ID → stack offset (negative from RBP, in bytes)
	StackSize int                 // total stack frame size needed
}

// Allocable registers (caller-saved first, then callee-saved).
// RSP and RBP are reserved.
var allocableRegs = []encoder.Reg{
	encoder.RAX,
	encoder.RCX,
	encoder.RDX,
	encoder.RSI,
	encoder.RDI,
	encoder.R8,
	encoder.R9,
	encoder.R10,
	encoder.R11,
	// Callee-saved (require save/restore):
	encoder.RBX,
	encoder.R12,
	encoder.R13,
	encoder.R14,
	encoder.R15,
}

// LiveInterval represents the live range of a virtual register.
type LiveInterval struct {
	ValueID int
	Start   int // instruction index where value is first defined
	End     int // instruction index of last use
}

// Allocate performs linear scan register allocation for a function.
func Allocate(fn *ir.Function) *Allocation {
	alloc := &Allocation{
		Reg:   make(map[int]encoder.Reg),
		Spill: make(map[int]int),
	}

	// Build a flat list of all instructions for linear ordering
	var instrs []*ir.Instruction
	for _, bb := range fn.Blocks {
		instrs = append(instrs, bb.Instrs...)
	}

	// Compute live intervals
	intervals := computeIntervals(fn, instrs)

	// Linear scan
	active := make([]LiveInterval, 0, len(allocableRegs))
	free := make([]encoder.Reg, len(allocableRegs))
	copy(free, allocableRegs)
	stackOffset := 0

	for _, interval := range intervals {
		// Expire old intervals
		newActive := active[:0]
		for _, a := range active {
			if a.End >= interval.Start {
				newActive = append(newActive, a)
			} else {
				// Return register to free pool
				free = append(free, alloc.Reg[a.ValueID])
			}
		}
		active = newActive

		if len(free) > 0 {
			// Assign a physical register
			reg := free[0]
			free = free[1:]
			alloc.Reg[interval.ValueID] = reg
			active = append(active, interval)
		} else {
			// Spill: assign a stack slot
			stackOffset += 8 // 8 bytes per slot (64-bit)
			alloc.Spill[interval.ValueID] = -stackOffset
		}
	}

	alloc.StackSize = stackOffset
	return alloc
}

// computeIntervals computes live intervals for all values in the function.
func computeIntervals(fn *ir.Function, instrs []*ir.Instruction) []LiveInterval {
	// Map value ID → first def and last use positions
	first := make(map[int]int)
	last := make(map[int]int)

	// Parameters start at position 0
	for _, param := range fn.Params {
		first[param.ID] = 0
		last[param.ID] = len(instrs)
	}

	for i, instr := range instrs {
		if instr.Result != nil {
			if _, ok := first[instr.Result.ID]; !ok {
				first[instr.Result.ID] = i
			}
			last[instr.Result.ID] = i
		}
		for _, arg := range instr.Args {
			if arg != nil && (arg.Kind == ir.ValInstr || arg.Kind == ir.ValParam) {
				if _, ok := first[arg.ID]; !ok {
					first[arg.ID] = i
				}
				if i > last[arg.ID] {
					last[arg.ID] = i
				}
			}
		}
	}

	var intervals []LiveInterval
	for id, start := range first {
		end := last[id]
		if end < start {
			end = start
		}
		intervals = append(intervals, LiveInterval{ValueID: id, Start: start, End: end})
	}

	// Sort by start point
	sortIntervals(intervals)
	return intervals
}

func sortIntervals(intervals []LiveInterval) {
	// Simple insertion sort (functions are small)
	for i := 1; i < len(intervals); i++ {
		key := intervals[i]
		j := i - 1
		for j >= 0 && intervals[j].Start > key.Start {
			intervals[j+1] = intervals[j]
			j--
		}
		intervals[j+1] = key
	}
}
