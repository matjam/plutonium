# Bodies for the functions main.p declares without one, so that the
# case's program links. They do nothing; halt never returns.
	.text
	.globl "main.wait"
"main.wait":
	ret
	.section .note.GNU-stack,"",@progbits
