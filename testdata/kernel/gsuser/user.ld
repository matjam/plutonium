/* Links a kernel-mode program as a Linux executable: the image at the usual
 * address and .percpu at address 0, not loaded (COPY), so that each per-CPU
 * symbol's value is its offset in a per-CPU block. The program sets up its
 * own blocks, so the initial image is not needed at run time. */
ENTRY(_start)

SECTIONS
{
	. = 0x400000;
	.text : { *(.text .text.*) }
	. = ALIGN(4096);
	.rodata : { *(.rodata .rodata.*) }
	. = ALIGN(4096);
	.data : { *(.data .data.*) }
	.bss : { *(.bss .bss.*) *(COMMON) }
	.percpu 0 (COPY) : { *(.percpu .percpu.*) }
	/DISCARD/ : { *(.note .note.*) *(.eh_frame .eh_frame_hdr) *(.comment) }
}
