# pluto/arch/x86: out-of-line bodies of the x86 intrinsics, used only when a
# function is called through a func value. Direct calls are expanded inline
# by the compiler. Arguments arrive in RDI, RSI; results go in RAX, RDX, RCX,
# RSI (abi.md section 3).
#pu:build amd64

	.text

.macro FUNC name
	.globl "pluto/arch/x86.\name"
	.type "pluto/arch/x86.\name", @function
"pluto/arch/x86.\name":
.endm

FUNC Cli
	cli
	ret

FUNC Sti
	sti
	ret

FUNC StiHlt
	sti
	hlt
	ret

FUNC Hlt
	hlt
	ret

FUNC Pause
	pause
	ret

FUNC SaveFlagsCli
	pushfq
	pop	%rax
	cli
	ret

FUNC RestoreFlags
	push	%rdi
	popfq
	ret

FUNC Inb
	mov	%edi, %edx
	xor	%eax, %eax
	inb	%dx, %al
	ret

FUNC Inw
	mov	%edi, %edx
	xor	%eax, %eax
	inw	%dx, %ax
	ret

FUNC Inl
	mov	%edi, %edx
	inl	%dx, %eax
	ret

FUNC Outb
	mov	%edi, %edx
	mov	%esi, %eax
	outb	%al, %dx
	ret

FUNC Outw
	mov	%edi, %edx
	mov	%esi, %eax
	outw	%ax, %dx
	ret

FUNC Outl
	mov	%edi, %edx
	mov	%esi, %eax
	outl	%eax, %dx
	ret

FUNC Rdmsr
	mov	%edi, %ecx
	rdmsr
	shl	$32, %rdx
	or	%rdx, %rax
	ret

FUNC Wrmsr
	mov	%edi, %ecx
	mov	%esi, %eax
	mov	%rsi, %rdx
	shr	$32, %rdx
	wrmsr
	ret

FUNC ReadCR0
	mov	%cr0, %rax
	ret

FUNC ReadCR2
	mov	%cr2, %rax
	ret

FUNC ReadCR3
	mov	%cr3, %rax
	ret

FUNC ReadCR4
	mov	%cr4, %rax
	ret

FUNC WriteCR0
	mov	%rdi, %cr0
	ret

FUNC WriteCR3
	mov	%rdi, %cr3
	ret

FUNC WriteCR4
	mov	%rdi, %cr4
	ret

FUNC Invlpg
	invlpg	(%rdi)
	ret

FUNC Lgdt
	lgdt	(%rdi)
	ret

FUNC Lidt
	lidt	(%rdi)
	ret

FUNC Ltr
	ltr	%di
	ret

# Cpuid returns eax, ebx, ecx, edx in RAX, RDX, RCX, RSI. RBX is
# callee-saved.
FUNC Cpuid
	push	%rbx
	mov	%edi, %eax
	mov	%esi, %ecx
	cpuid
	mov	%edx, %esi
	mov	%ebx, %edx
	pop	%rbx
	ret

FUNC Rdtsc
	rdtsc
	shl	$32, %rdx
	or	%rdx, %rax
	ret

FUNC Rdtscp
	rdtscp
	shl	$32, %rdx
	or	%rdx, %rax
	mov	%ecx, %edx
	ret

FUNC Swapgs
	swapgs
	ret

FUNC ReadGSBase
	mov	$0xC0000101, %ecx
	rdmsr
	shl	$32, %rdx
	or	%rdx, %rax
	ret

FUNC WriteGSBase
	mov	$0xC0000101, %ecx
	mov	%edi, %eax
	mov	%rdi, %rdx
	shr	$32, %rdx
	wrmsr
	ret

FUNC GSLoad64
	mov	%gs:(%rdi), %rax
	ret

FUNC GSLoad32
	mov	%gs:(%rdi), %eax
	ret

FUNC GSStore64
	mov	%rsi, %gs:(%rdi)
	ret

FUNC GSStore32
	mov	%esi, %gs:(%rdi)
	ret

FUNC FPUBegin
	clts
	fxsave64 (%rdi)
	ret

FUNC FPUEnd
	fxrstor64 (%rdi)
	ret

	.section .note.GNU-stack,"",@progbits
