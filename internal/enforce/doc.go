// Package enforce prepares and attaches the bounded Linux kernel projection
// of a validated policy snapshot.
package enforce

// Generate the CO-RE object in the configured Linux guest with its pinned
// clang/LLVM, bpftool-generated vmlinux.h, and repository-pinned bpf2go.
//go:generate go tool bpf2go -tags linux -target bpfel -cc clang enforce ../../bpf/enforce.bpf.c -- -I../../bpf
