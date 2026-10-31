# Plutonium runtime: the paging check of checked freestanding builds.
#pu:build gencheck && none && amd64

	.text

# fiveLevelPaging() bool reports whether CR4.LA57 (bit 12) is set. Only
# ring 0 may read CR4, so in any other ring it reports false.
	.globl "pluto/rt.fiveLevelPaging"
	.type "pluto/rt.fiveLevelPaging", @function
"pluto/rt.fiveLevelPaging":
	xorl	%eax, %eax
	movw	%cs, %cx
	testb	$3, %cl
	jnz	1f
	movq	%cr4, %rcx
	btq	$12, %rcx
	setc	%al
1:	ret

	.section .note.GNU-stack,"",@progbits
