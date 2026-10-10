# Linux x86-64 system call stubs for package pluto/syscall.
#pu:build linux && amd64

	.text

# Syscall(num, a1, a2, a3 uintptr) uintptr
	.globl "pluto/syscall.Syscall"
	.type "pluto/syscall.Syscall", @function
"pluto/syscall.Syscall":
	movq	%rdi, %rax
	movq	%rsi, %rdi
	movq	%rdx, %rsi
	movq	%rcx, %rdx
	syscall
	ret

# Syscall6(num, a1, a2, a3, a4, a5, a6 uintptr) uintptr
	.globl "pluto/syscall.Syscall6"
	.type "pluto/syscall.Syscall6", @function
"pluto/syscall.Syscall6":
	movq	%rdi, %rax
	movq	%rsi, %rdi
	movq	%rdx, %rsi
	movq	%rcx, %rdx
	movq	%r8, %r10
	movq	%r9, %r8
	movq	8(%rsp), %r9
	syscall
	ret

	.section .note.GNU-stack,"",@progbits
