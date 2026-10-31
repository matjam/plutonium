# softInt() raises interrupt 0x40.

	.text
	.globl "main.softInt"
"main.softInt":
	int	$0x40
	ret

	.section .note.GNU-stack,"",@progbits
