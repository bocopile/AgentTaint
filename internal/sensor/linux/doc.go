// Package linux implements the Linux eBPF observer.
package linux

// Generate the process observer from the pinned Linux toolchain. Run from a
// Linux guest with clang, llvm-strip, bpftool, and kernel BTF available.
//go:generate go tool bpf2go -tags linux -target bpfel,bpfeb -cc clang process ../../../bpf/process.bpf.c -- -D__TARGET_ARCH_arm64 -I../../../bpf
