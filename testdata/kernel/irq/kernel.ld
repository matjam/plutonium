/* Flat multiboot image loaded at 1 MiB; see boot_amd64.s. */
ENTRY(start32)

SECTIONS
{
	. = 0x100000;
	__load_start = .;
	.multiboot : { KEEP(*(.multiboot)) }
	.text : { *(.boot) *(.text .text.*) }
	.rodata : { *(.rodata .rodata.*) }
	.data : { *(.data .data.*) }
	__load_end = .;
	.bss : { *(.bss .bss.*) *(COMMON) }
	__bss_end = .;
	/DISCARD/ : { *(.note .note.*) *(.eh_frame .eh_frame_hdr) *(.comment) }
}
