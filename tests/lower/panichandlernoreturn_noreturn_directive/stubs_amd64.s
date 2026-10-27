# Bodies for the functions main.p declares without one, so that the
# case's program links. They do nothing; halt never returns.
	.text
	.globl "main.halt"
"main.halt":
1:	hlt
	jmp	1b
	.section .note.GNU-stack,"",@progbits
