# Plutonium runtime: freestanding x86-64 support.
#pu:build none && amd64

	.text

# halt disables interrupts and stops the processor for good.
	.globl "pluto/rt.halt"
	.type "pluto/rt.halt", @function
"pluto/rt.halt":
	cli
1:	hlt
	jmp	1b

	.section .note.GNU-stack,"",@progbits
