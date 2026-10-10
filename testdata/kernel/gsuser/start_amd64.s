# Entry point and system calls for the GS test program, which is built in
# kernel mode but runs as a Linux process.

	.text
	.globl _start
_start:
	andq	$-16, %rsp
	call	pluto.main
	movq	%rax, %rdi
	movl	$60, %eax		# exit
	syscall

# setGS(base uintptr): arch_prctl(ARCH_SET_GS, base).
	.globl "main.setGS"
"main.setGS":
	movq	%rdi, %rsi
	movl	$0x1001, %edi
	movl	$158, %eax
	syscall
	ret

# exit(code int) ends the process.
	.globl "main.exit"
"main.exit":
	movl	$60, %eax		# exit
	syscall
	ud2

# write(p unsafe.Pointer, n int): write(2, p, n).
	.globl "main.write"
"main.write":
	movq	%rsi, %rdx
	movq	%rdi, %rsi
	movl	$2, %edi
	movl	$1, %eax
	syscall
	ret

	.section .note.GNU-stack,"",@progbits
