#include "vmlinux.h"
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>

#include "events.h"
#include "maps.h"

static __always_inline void count_drop(__u32 reason)
{
	__u64 *count = bpf_map_lookup_elem(&process_drops, &reason);
	if (count)
		__sync_fetch_and_add(count, 1);
}

static __always_inline __u64 process_birth(struct task_struct *task)
{
	__u32 tid = BPF_CORE_READ(task, pid);
	__u64 *known = bpf_map_lookup_elem(&process_births, &tid);
	if (known)
		return *known;

	struct task_struct *leader = BPF_CORE_READ(task, group_leader);
	if (!leader)
		return 0;
	return BPF_CORE_READ(leader, start_boottime);
}

static __always_inline int read_process(struct task_struct *task,
					struct agenttaint_raw_process *out,
					__u64 scope_id, __u64 birth_id)
{
	struct signal_struct *signal = BPF_CORE_READ(task, signal);
	if (!signal)
		return -1;
	struct pid *pgrp = BPF_CORE_READ(signal, pids[2]);
	struct pid *session = BPF_CORE_READ(signal, pids[3]);
	if (!pgrp || !session)
		return -1;

	__builtin_memset(out, 0, sizeof(*out));
	out->pid = BPF_CORE_READ(task, tgid);
	struct task_struct *parent = BPF_CORE_READ(task, real_parent);
	if (!parent)
		return -1;
	out->ppid = BPF_CORE_READ(parent, tgid);
	out->pgid = BPF_CORE_READ(pgrp, numbers[0].nr);
	out->session_id = BPF_CORE_READ(session, numbers[0].nr);
	out->birth_id = birth_id;
	out->scope_id = scope_id;
	if (!out->pid || !out->pgid || !out->session_id || !birth_id || !scope_id)
		return -1;
	if (bpf_core_read(out->comm, sizeof(out->comm), &task->comm) < 0)
		return -1;
	return 0;
}

static __always_inline bool tracking_enabled(void)
{
	__u32 zero = 0;
	__u32 *enabled = bpf_map_lookup_elem(&process_tracking_enabled, &zero);
	return enabled && *enabled != 0;
}

static __always_inline __u64 current_scope(void)
{
	__u32 zero = 0;
	__u64 *scope = bpf_map_lookup_elem(&process_scope, &zero);
	return scope ? *scope : 0;
}

static __always_inline void init_header(struct agenttaint_raw_header *header,
					__u8 kind, __u8 flags, __u16 length)
{
	__builtin_memset(header, 0, sizeof(*header));
	header->magic = AGENTTAINT_RAW_MAGIC;
	header->version = AGENTTAINT_RAW_VERSION;
	header->length = length;
	header->monotonic_ns = bpf_ktime_get_ns();
	header->kind = kind;
	header->flags = flags;
}

SEC("tp_btf/sched_process_fork")
int BPF_PROG(on_process_fork, struct task_struct *parent, struct task_struct *child)
{
	__u32 parent_tid = BPF_CORE_READ(parent, pid);
	__u32 child_tid = BPF_CORE_READ(child, pid);
	__u32 child_tgid = BPF_CORE_READ(child, tgid);
	bool scoped = tracking_enabled();
	__u64 *parent_scope = scoped ? bpf_map_lookup_elem(&tracked_processes, &parent_tid) : 0;
	if (scoped && !parent_scope)
		return 0;
	__u64 scope_id = scoped ? *parent_scope : current_scope();
	__u64 parent_birth = process_birth(parent);
	if (!scope_id || !parent_birth || !parent_tid || !child_tid || !child_tgid) {
		count_drop(AGENTTAINT_DROP_PROCESS_CAPTURE);
		return 0;
	}

	__u64 child_birth = parent_birth;
	if (child_tgid != BPF_CORE_READ(parent, tgid))
		child_birth = BPF_CORE_READ(child, start_boottime);
	if (!child_birth || bpf_map_update_elem(&process_births, &child_tid, &child_birth, BPF_ANY) < 0) {
		count_drop(AGENTTAINT_DROP_PROCESS_CAPTURE);
		return 0;
	}
	if (child_tgid != BPF_CORE_READ(parent, tgid) &&
	    bpf_map_update_elem(&process_births, &child_tgid, &child_birth, BPF_ANY) < 0) {
		count_drop(AGENTTAINT_DROP_PROCESS_CAPTURE);
		return 0;
	}
	if (scoped && (bpf_map_update_elem(&tracked_processes, &child_tid, &scope_id, BPF_ANY) < 0 ||
	    (child_tgid != BPF_CORE_READ(parent, tgid) &&
	     bpf_map_update_elem(&tracked_processes, &child_tgid, &scope_id, BPF_ANY) < 0))) {
		count_drop(AGENTTAINT_DROP_PROCESS_CAPTURE);
		return 0;
	}
	/* Thread creation shares the process identity and is not a core fork event. */
	if (child_tgid == BPF_CORE_READ(parent, tgid))
		return 0;

	struct agenttaint_raw_fork *event = bpf_ringbuf_reserve(&process_events, sizeof(*event), 0);
	if (!event) {
		count_drop(AGENTTAINT_DROP_PROCESS_RING);
		return 0;
	}
	if (read_process(parent, &event->parent, scope_id, parent_birth) < 0 ||
	    read_process(child, &event->child, scope_id, child_birth) < 0) {
		bpf_ringbuf_discard(event, 0);
		count_drop(AGENTTAINT_DROP_PROCESS_CAPTURE);
		return 0;
	}
	init_header(&event->header, AGENTTAINT_PROCESS_FORK, 0, sizeof(*event));
	bpf_ringbuf_submit(event, 0);
	return 0;
}

SEC("tp_btf/sched_process_exec")
int BPF_PROG(on_process_exec, struct task_struct *task, pid_t old_pid, struct linux_binprm *bprm)
{
	__u32 tid = BPF_CORE_READ(task, pid);
	__u32 tgid = BPF_CORE_READ(task, tgid);
	bool scoped = tracking_enabled();
	__u64 *tracked_scope = scoped ? bpf_map_lookup_elem(&tracked_processes, &tid) : 0;
	if (scoped && !tracked_scope && old_pid > 0) {
		__u32 previous_tid = old_pid;
		tracked_scope = bpf_map_lookup_elem(&tracked_processes, &previous_tid);
	}
	if (scoped && !tracked_scope)
		tracked_scope = bpf_map_lookup_elem(&tracked_processes, &tgid);
	if (scoped && !tracked_scope)
		return 0;
	__u64 scope_id = scoped ? *tracked_scope : current_scope();
	__u64 birth_id = 0;
	if (old_pid > 0) {
		__u32 previous_tid = old_pid;
		__u64 *previous = bpf_map_lookup_elem(&process_births, &previous_tid);
		if (previous)
			birth_id = *previous;
		if (previous_tid != tid && previous_tid != tgid)
			bpf_map_delete_elem(&process_births, &previous_tid);
	}
	if (!birth_id)
		birth_id = process_birth(task);
	if (!scope_id || !birth_id || !tid || !tgid ||
	    (scoped && (bpf_map_update_elem(&tracked_processes, &tid, &scope_id, BPF_ANY) < 0 ||
	     bpf_map_update_elem(&tracked_processes, &tgid, &scope_id, BPF_ANY) < 0)) ||
	    bpf_map_update_elem(&process_births, &tid, &birth_id, BPF_ANY) < 0 ||
	    bpf_map_update_elem(&process_births, &tgid, &birth_id, BPF_ANY) < 0) {
		count_drop(AGENTTAINT_DROP_PROCESS_CAPTURE);
		return 0;
	}

	struct agenttaint_raw_exec *event = bpf_ringbuf_reserve(&process_events, sizeof(*event), 0);
	if (!event) {
		count_drop(AGENTTAINT_DROP_PROCESS_RING);
		return 0;
	}
	__builtin_memset(event, 0, sizeof(*event));
	if (read_process(task, &event->process, scope_id, birth_id) < 0) {
		bpf_ringbuf_discard(event, 0);
		count_drop(AGENTTAINT_DROP_PROCESS_CAPTURE);
		return 0;
	}
	long copied = bpf_probe_read_kernel_str(event->path, sizeof(event->path),
						       BPF_CORE_READ(bprm, filename));
	if (copied <= 0) {
		bpf_ringbuf_discard(event, 0);
		count_drop(AGENTTAINT_DROP_PROCESS_CAPTURE);
		return 0;
	}
	__u8 flags = copied >= sizeof(event->path) ? AGENTTAINT_PATH_TRUNCATED : 0;
	init_header(&event->header, AGENTTAINT_PROCESS_EXEC, flags, sizeof(*event));
	bpf_ringbuf_submit(event, 0);
	return 0;
}

SEC("tp_btf/sched_process_exit")
int BPF_PROG(on_process_exit, struct task_struct *task, bool group_dead)
{
	__u32 tid = BPF_CORE_READ(task, pid);
	__u32 tgid = BPF_CORE_READ(task, tgid);
	bool scoped = tracking_enabled();
	__u64 *tracked_scope = scoped ? bpf_map_lookup_elem(&tracked_processes, &tid) : 0;
	if (scoped && !tracked_scope) {
		bpf_map_delete_elem(&pending_file_opens, &tid);
		bpf_map_delete_elem(&pending_connects, &tid);
		return 0;
	}
	__u64 scope_id = scoped ? *tracked_scope : current_scope();
	bpf_map_delete_elem(&pending_file_opens, &tid);
	bpf_map_delete_elem(&pending_connects, &tid);
	if (!group_dead) {
		bpf_map_delete_elem(&process_births, &tid);
		if (scoped)
			bpf_map_delete_elem(&tracked_processes, &tid);
		return 0;
	}
	if (tgid != tid) {
		bpf_map_delete_elem(&pending_file_opens, &tgid);
		bpf_map_delete_elem(&pending_connects, &tgid);
		if (scoped)
			bpf_map_delete_elem(&tracked_processes, &tgid);
	}
	__u64 birth_id = process_birth(task);
	if (!scope_id || !birth_id || !tid || !tgid) {
		count_drop(AGENTTAINT_DROP_PROCESS_CAPTURE);
		return 0;
	}
	struct agenttaint_raw_exit *event = bpf_ringbuf_reserve(&process_events, sizeof(*event), 0);
	if (!event) {
		count_drop(AGENTTAINT_DROP_PROCESS_RING);
		return 0;
	}
	if (read_process(task, &event->process, scope_id, birth_id) < 0) {
		bpf_ringbuf_discard(event, 0);
		count_drop(AGENTTAINT_DROP_PROCESS_CAPTURE);
		return 0;
	}
	init_header(&event->header, AGENTTAINT_PROCESS_EXIT, 0, sizeof(*event));
	bpf_ringbuf_submit(event, 0);
	bpf_map_delete_elem(&process_births, &tid);
	bpf_map_delete_elem(&process_births, &tgid);
	if (scoped) {
		bpf_map_delete_elem(&tracked_processes, &tid);
		bpf_map_delete_elem(&tracked_processes, &tgid);
	}
	return 0;
}

#include "file.bpf.c"
#include "network.bpf.c"

#define AGENTTAINT_ARM64_NR_OPENAT 56
#define AGENTTAINT_ARM64_NR_CONNECT 203
#define AGENTTAINT_ARM64_NR_OPENAT2 437

static __always_inline int arm64_syscall_arg(struct pt_regs *regs, __u32 index, __u64 *value)
{
	if (!regs || index >= 6)
		return -1;
	switch (index) {
	case 0:
		return bpf_core_read(value, sizeof(*value), &regs->orig_x0);
	case 1:
		return bpf_core_read(value, sizeof(*value), &regs->regs[1]);
	case 2:
		return bpf_core_read(value, sizeof(*value), &regs->regs[2]);
	case 3:
		return bpf_core_read(value, sizeof(*value), &regs->regs[3]);
	case 4:
		return bpf_core_read(value, sizeof(*value), &regs->regs[4]);
	case 5:
		return bpf_core_read(value, sizeof(*value), &regs->regs[5]);
	default:
		return -1;
	}
}

SEC("tp_btf/sys_enter")
int BPF_PROG(on_sys_enter, struct pt_regs *regs, long syscall_nr)
{
	__u64 arg1 = 0;
	__u64 arg2 = 0;
	__u64 arg3 = 0;

	if (syscall_nr == AGENTTAINT_ARM64_NR_OPENAT) {
		if (arm64_syscall_arg(regs, 1, &arg1) < 0 || arm64_syscall_arg(regs, 2, &arg2) < 0) {
			file_count_drop(AGENTTAINT_DROP_FILE_SYSCALL_ARGS);
			return 0;
		}
		file_enter_openat((const char *)(unsigned long)arg1, arg2, true);
	} else if (syscall_nr == AGENTTAINT_ARM64_NR_OPENAT2) {
		if (arm64_syscall_arg(regs, 1, &arg1) < 0 || arm64_syscall_arg(regs, 2, &arg2) < 0 ||
		    arm64_syscall_arg(regs, 3, &arg3) < 0) {
			file_count_drop(AGENTTAINT_DROP_FILE_SYSCALL_ARGS);
			return 0;
		}
		__u64 flags = 0;
		bool valid = arg3 >= sizeof(flags) &&
			bpf_probe_read_user(&flags, sizeof(flags), (const void *)(unsigned long)arg2) == 0;
		file_enter_openat((const char *)(unsigned long)arg1, flags, valid);
	} else if (syscall_nr == AGENTTAINT_ARM64_NR_CONNECT) {
		if (arm64_syscall_arg(regs, 1, &arg1) < 0 || arm64_syscall_arg(regs, 2, &arg2) < 0) {
			network_count_drop(AGENTTAINT_DROP_NETWORK_CAPTURE);
			return 0;
		}
		network_enter_connect((const void *)(unsigned long)arg1, arg2);
	}
	return 0;
}

SEC("tp_btf/sys_exit")
int BPF_PROG(on_sys_exit, struct pt_regs *regs, long result)
{
	file_exit_openat(result);
	network_exit_connect(result);
	return 0;
}

char LICENSE[] SEC("license") = "GPL";
