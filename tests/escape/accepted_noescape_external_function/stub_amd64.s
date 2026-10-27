# A body for the external function main.p declares, so that the case's
# program links. It does nothing.
	.text
	.globl	ext_read
ext_read:
	xorl	%eax, %eax
	ret
	.section .note.GNU-stack,"",@progbits
