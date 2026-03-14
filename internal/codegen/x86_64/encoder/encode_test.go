package encoder_test

import (
	"bytes"
	"testing"

	"github.com/matjam/plutonium/internal/codegen/x86_64/encoder"
)

func TestMovRegReg64(t *testing.T) {
	// MOV RAX, RBX = 48 89 D8
	got := encoder.EncMOV_RR(encoder.RAX, encoder.RBX, encoder.W64)
	want := []byte{0x48, 0x89, 0xD8}
	if !bytes.Equal(got, want) {
		t.Errorf("MOV RAX, RBX: got %x, want %x", got, want)
	}
}

func TestMovRegReg32(t *testing.T) {
	// MOV EAX, EBX = 89 D8
	got := encoder.EncMOV_RR(encoder.RAX, encoder.RBX, encoder.W32)
	want := []byte{0x89, 0xD8}
	if !bytes.Equal(got, want) {
		t.Errorf("MOV EAX, EBX: got %x, want %x", got, want)
	}
}

func TestMovRegImm64(t *testing.T) {
	// MOV RAX, 1 = 48 B8 01 00 00 00 00 00 00 00
	got := encoder.EncMOV_RI(encoder.RAX, 1, encoder.W64)
	want := []byte{0x48, 0xB8, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	if !bytes.Equal(got, want) {
		t.Errorf("MOV RAX, 1: got %x, want %x", got, want)
	}
}

func TestMovRegImm32(t *testing.T) {
	// MOV EAX, 42 = B8 2A 00 00 00
	got := encoder.EncMOV_RI(encoder.RAX, 42, encoder.W32)
	want := []byte{0xB8, 0x2A, 0x00, 0x00, 0x00}
	if !bytes.Equal(got, want) {
		t.Errorf("MOV EAX, 42: got %x, want %x", got, want)
	}
}

func TestAddRR64(t *testing.T) {
	// ADD RAX, RBX = 48 01 D8
	got := encoder.EncADD_RR(encoder.RAX, encoder.RBX, encoder.W64)
	want := []byte{0x48, 0x01, 0xD8}
	if !bytes.Equal(got, want) {
		t.Errorf("ADD RAX, RBX: got %x, want %x", got, want)
	}
}

func TestSubRR64(t *testing.T) {
	// SUB RAX, RBX = 48 29 D8
	got := encoder.EncSUB_RR(encoder.RAX, encoder.RBX, encoder.W64)
	want := []byte{0x48, 0x29, 0xD8}
	if !bytes.Equal(got, want) {
		t.Errorf("SUB RAX, RBX: got %x, want %x", got, want)
	}
}

func TestAddRI_imm8(t *testing.T) {
	// ADD RAX, 5 = 48 83 C0 05
	got := encoder.EncADD_RI(encoder.RAX, 5, encoder.W64)
	want := []byte{0x48, 0x83, 0xC0, 0x05}
	if !bytes.Equal(got, want) {
		t.Errorf("ADD RAX, 5: got %x, want %x", got, want)
	}
}

func TestSubRI_imm8(t *testing.T) {
	// SUB RSP, 32 = 48 83 EC 20
	got := encoder.EncSUB_RI(encoder.RSP, 32, encoder.W64)
	want := []byte{0x48, 0x83, 0xEC, 0x20}
	if !bytes.Equal(got, want) {
		t.Errorf("SUB RSP, 32: got %x, want %x", got, want)
	}
}

func TestPushPop(t *testing.T) {
	// PUSH RBP = 55
	got := encoder.EncPUSH(encoder.RBP)
	if !bytes.Equal(got, []byte{0x55}) {
		t.Errorf("PUSH RBP: got %x, want 55", got)
	}
	// POP RBP = 5D
	got = encoder.EncPOP(encoder.RBP)
	if !bytes.Equal(got, []byte{0x5D}) {
		t.Errorf("POP RBP: got %x, want 5D", got)
	}
}

func TestRet(t *testing.T) {
	got := encoder.EncRET()
	if !bytes.Equal(got, []byte{0xC3}) {
		t.Errorf("RET: got %x, want C3", got)
	}
}

func TestSyscall(t *testing.T) {
	got := encoder.EncSYSCALL()
	if !bytes.Equal(got, []byte{0x0F, 0x05}) {
		t.Errorf("SYSCALL: got %x, want 0F 05", got)
	}
}

func TestCallRel32(t *testing.T) {
	// CALL 0 (relative offset 0) = E8 00 00 00 00
	got := encoder.EncCALL_REL32(0)
	want := []byte{0xE8, 0x00, 0x00, 0x00, 0x00}
	if !bytes.Equal(got, want) {
		t.Errorf("CALL rel32: got %x, want %x", got, want)
	}
}

func TestJmpRel32(t *testing.T) {
	got := encoder.EncJMP_REL32(0)
	want := []byte{0xE9, 0x00, 0x00, 0x00, 0x00}
	if !bytes.Equal(got, want) {
		t.Errorf("JMP rel32: got %x, want %x", got, want)
	}
}

func TestJccRel32(t *testing.T) {
	// JE rel32 = 0F 84 00 00 00 00
	got := encoder.EncJcc_REL32(encoder.CondE, 0)
	want := []byte{0x0F, 0x84, 0x00, 0x00, 0x00, 0x00}
	if !bytes.Equal(got, want) {
		t.Errorf("JE rel32: got %x, want %x", got, want)
	}
}

func TestXorZero(t *testing.T) {
	// XOR EAX, EAX = 31 C0
	got := encoder.EncXOR_RR_zero(encoder.RAX)
	want := []byte{0x31, 0xC0}
	if !bytes.Equal(got, want) {
		t.Errorf("XOR EAX, EAX: got %x, want %x", got, want)
	}
}

func TestExtendedReg(t *testing.T) {
	// MOV R8, R9 = 4D 89 C8
	got := encoder.EncMOV_RR(encoder.R8, encoder.R9, encoder.W64)
	want := []byte{0x4D, 0x89, 0xC8}
	if !bytes.Equal(got, want) {
		t.Errorf("MOV R8, R9: got %x, want %x", got, want)
	}
}
