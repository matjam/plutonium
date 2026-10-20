package build

import (
	"debug/elf"
	"fmt"

	"github.com/matjam/plutonium/internal/diag"
)

// checkPerCPU verifies, in a linked ELF executable, that the per-CPU block
// starts at address 0 with its self word, as GS-relative accesses assume.
// A linker script that places .percpu elsewhere would make every per-CPU
// access wrong, so this is an error rather than a warning.
//
// The self-hosted compiler replaces this file with percpu.p, which reads the
// symbol table itself (selfhost/elf has the types, not the reader).
func checkPerCPU(path string) error {
	f, err := elf.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	syms, err := f.Symbols()
	if err != nil {
		return nil // no symbol table: nothing to check
	}
	for _, s := range syms {
		if s.Name == perCPUSelf && s.Value != 0 {
			return &diag.Diagnostic{Code: diag.PerCPUNotAtZero,
				Msg: fmt.Sprintf("%s: %s is at %#x: the linker script must link section .percpu at address 0, with the Plutonium object first (abi.md section 10.6)", path, perCPUSelf, s.Value)}
		}
		if s.Name == perCPUTask && s.Value != 8 {
			return &diag.Diagnostic{Code: diag.PerCPUNotAtZero,
				Msg: fmt.Sprintf("%s: %s is at %#x, not 8: the linker script must link section .percpu at address 0, with the Plutonium object first (abi.md sections 10.6 and 12.3)", path, perCPUTask, s.Value)}
		}
	}
	return nil
}
