# softInt() raises interrupt 0x80.

	.text
	.globl "main.softInt"
"main.softInt":
	int	$0x80
	ret

	.section .note.GNU-stack,"",@progbits
