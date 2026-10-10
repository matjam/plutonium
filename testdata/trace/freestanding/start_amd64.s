# Entry point, console and halt for a freestanding program run as a Linux
# process.

	.text
	.globl _start
_start:
	andq	$-16, %rsp
	call	pluto.main
	movq	%rax, %rdi
	movl	$231, %eax		# exit_group
	syscall
	ud2

# consoleWrite(p unsafe.Pointer, n int) writes to standard error.
	.globl "main.consoleWrite"
"main.consoleWrite":
	movq	%rsi, %rdx
	movq	%rdi, %rsi
	movl	$2, %edi
	movl	$1, %eax		# write
	syscall
	ret

# halt ends the process with status 3.
	.globl "main.halt"
"main.halt":
	movl	$3, %edi
	movl	$231, %eax
	syscall
	ud2

	.section .note.GNU-stack,"",@progbits
