# Plutonium

Plutonium is a freestanding, kernel-safe subset-of-Go compiler designed for systems programming where safety and predictability are required.

## Overview

- Source files use the `.pu` extension
- All compiler directives use the `//pu:` prefix
- Targets x86_64 Linux initially; aarch64 support is planned
- No garbage collector
- No goroutines, channels, or concurrency primitives
- No reflection
- No hidden heap allocation — every allocation is explicit

Plutonium is suited for writing kernel modules, bootloaders, embedded firmware, and other freestanding environments where runtime overhead is unacceptable.

## Status

Early development. Not usable for production work.

## Build

```bash
go build ./cmd/pluto/
```

This produces the `pluto` binary.

## Usage

```
pluto [command] [flags]
```

Run `pluto --help` for available commands.
