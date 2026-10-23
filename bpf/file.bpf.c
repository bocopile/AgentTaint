#include "vmlinux.h"
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_helpers.h>

#include "events.h"
#include "maps.h"

#ifndef O_ACCMODE
#define O_ACCMODE 00000003
#endif
#ifndef O_PATH
#define O_PATH 010000000
#endif

static __always_inline void file_count_drop(__u32 reason)
{
	__u64 *count = bpf_map_lookup_elem(&process_drops, &reason);
	if (count)
		__sync_fetch_and_add(count, 1);
}

static __always_inline int file_read_process(struct task_struct *task,
						     struct agenttaint_raw_process *out,
						     __u64 scope_id, __u64 birth_id)
{
	struct signal_struct *signal = BPF_CORE_READ(task, signal);
	if (!signal)
		return -1;
	struct pid *pgrp = BPF_CORE_READ(signal, pids[2]);
	struct pid *session = BPF_CORE_READ(signal, pids[3]);
	struct task_struct *parent = BPF_CORE_READ(task, real_parent);
	if (!pgrp || !session || !parent)
		return -1;

	__builtin_memset(out, 0, sizeof(*out));
	out->pid = BPF_CORE_READ(task, tgid);
	out->ppid = BPF_CORE_READ(parent, tgid);
	out->pgid = BPF_CORE_READ(pgrp, numbers[0].nr);
	out->session_id = BPF_CORE_READ(session, numbers[0].nr);
	out->birth_id = birth_id;
	out->scope_id = scope_id;
	if (!out->pid || !out->pgid || !out->session_id || !birth_id || !scope_id)
		return -1;
	return bpf_core_read(out->comm, sizeof(out->comm), &task->comm);
}

static __always_inline int file_current_process(struct agenttaint_raw_process *out)
{
	struct task_struct *task = bpf_get_current_task_btf();
	if (!task)
		return -1;
	__u32 tid = BPF_CORE_READ(task, pid);
	__u64 *known = bpf_map_lookup_elem(&process_births, &tid);
	__u64 birth_id = 0;
	if (known) {
		birth_id = *known;
	} else {
		struct task_struct *leader = BPF_CORE_READ(task, group_leader);
		if (leader)
			birth_id = BPF_CORE_READ(leader, start_boottime);
	}
	__u32 zero = 0;
	__u32 *enabled = bpf_map_lookup_elem(&process_tracking_enabled, &zero);
	__u64 *scope = enabled && *enabled ? bpf_map_lookup_elem(&tracked_processes, &tid) : 0;
	if (enabled && *enabled && !scope)
		return -2;
	if (!scope) {
		__u64 *global_scope = bpf_map_lookup_elem(&process_scope, &zero);
		scope = global_scope;
	}
	return file_read_process(task, out, scope ? *scope : 0, birth_id);
}

static __always_inline __u8 file_access_from_flags(__u64 flags, bool valid)
{
	if (!valid || (flags & O_PATH))
		return AGENTTAINT_ACCESS_UNKNOWN;
	switch (flags & O_ACCMODE) {
	case 0:
		return AGENTTAINT_ACCESS_READ;
	case 1:
		return AGENTTAINT_ACCESS_WRITE;
	case 2:
		return AGENTTAINT_ACCESS_READ_WRITE;
	default:
		return AGENTTAINT_ACCESS_UNKNOWN;
	}
}

static __always_inline void file_enter_openat(const char *filename, __u64 flags,
						      bool flags_valid, __u8 syscall_kind)
{
	__u32 tid = (__u32)bpf_get_current_pid_tgid();
	struct agenttaint_pending_file_open pending = {};
	if (!tid) {
		file_count_drop(AGENTTAINT_DROP_FILE_CAPTURE);
		return;
	}
	int process_status = file_current_process(&pending.process);
	if (process_status == -2)
		return;
	if (process_status < 0) {
		file_count_drop(AGENTTAINT_DROP_FILE_PROCESS);
		return;
	}
	pending.monotonic_ns = bpf_ktime_get_ns();
	pending.access = file_access_from_flags(flags, flags_valid);
	pending.syscall_kind = syscall_kind;
	long copied = bpf_probe_read_user_str(pending.path, sizeof(pending.path), filename);
	if (copied <= 0) {
		pending.flags |= AGENTTAINT_PENDING_PATH_CAPTURE_FAILED;
		pending.path_read_result = (__s32)copied;
	} else {
		if (copied >= sizeof(pending.path))
			pending.flags |= AGENTTAINT_PATH_TRUNCATED;
		if (pending.path[0] != '/')
			pending.flags |= AGENTTAINT_PATH_RELATIVE;
	}
	if (bpf_map_update_elem(&pending_file_opens, &tid, &pending, BPF_ANY) < 0)
		file_count_drop(AGENTTAINT_DROP_FILE_PENDING);
}

static __always_inline void file_exit_openat(long result)
{
	__u32 tid = (__u32)bpf_get_current_pid_tgid();
	struct agenttaint_pending_file_open *pending = bpf_map_lookup_elem(&pending_file_opens, &tid);
	if (!pending)
		return;
	if (pending->flags & AGENTTAINT_PENDING_PATH_CAPTURE_FAILED) {
		struct agenttaint_raw_file_open *diagnostic =
			bpf_ringbuf_reserve(&process_events, sizeof(*diagnostic), 0);
		if (diagnostic) {
			__s16 helper_result = (__s16)pending->path_read_result;
			__s32 syscall_result = (__s32)result;
			__builtin_memset(diagnostic, 0, sizeof(*diagnostic));
			diagnostic->process = pending->process;
			diagnostic->access = pending->syscall_kind;
			__builtin_memcpy(diagnostic->reserved, &helper_result,
					 sizeof(helper_result));
			__builtin_memcpy(&diagnostic->reserved[2], &syscall_result,
					 sizeof(syscall_result));
			diagnostic->header.magic = AGENTTAINT_RAW_MAGIC;
			diagnostic->header.version = AGENTTAINT_RAW_VERSION;
			diagnostic->header.length = sizeof(*diagnostic);
			diagnostic->header.monotonic_ns = pending->monotonic_ns;
			diagnostic->header.kind = AGENTTAINT_FILE_OPEN;
			diagnostic->header.flags = AGENTTAINT_PATH_CAPTURE_FAILED;
			bpf_ringbuf_submit(diagnostic, 0);
		} else {
			file_count_drop(AGENTTAINT_DROP_FILE_RING);
		}
		if (pending->syscall_kind == AGENTTAINT_FILE_SYSCALL_OPENAT) {
			if (result >= 0)
				file_count_drop(AGENTTAINT_DROP_FILE_OPENAT_PATH_SUCCESS);
			else
				file_count_drop(AGENTTAINT_DROP_FILE_OPENAT_PATH_OTHER_ERROR);
		} else if (pending->syscall_kind == AGENTTAINT_FILE_SYSCALL_OPENAT2) {
			if (result >= 0)
				file_count_drop(AGENTTAINT_DROP_FILE_OPENAT2_PATH_SUCCESS);
			else
				file_count_drop(AGENTTAINT_DROP_FILE_OPENAT2_PATH_OTHER_ERROR);
		}
		bpf_map_delete_elem(&pending_file_opens, &tid);
		return;
	}
	struct agenttaint_raw_file_open *event = bpf_ringbuf_reserve(&process_events, sizeof(*event), 0);
	if (!event) {
		bpf_map_delete_elem(&pending_file_opens, &tid);
		file_count_drop(AGENTTAINT_DROP_FILE_RING);
		return;
	}
	__builtin_memset(event, 0, sizeof(*event));
	event->process = pending->process;
	__builtin_memcpy(event->path, pending->path, sizeof(event->path));
	event->access = pending->access;
	__u8 flags = pending->flags;
	if (result < 0)
		flags |= AGENTTAINT_OPERATION_FAILED;
	__builtin_memset(&event->header, 0, sizeof(event->header));
	event->header.magic = AGENTTAINT_RAW_MAGIC;
	event->header.version = AGENTTAINT_RAW_VERSION;
	event->header.length = sizeof(*event);
	event->header.monotonic_ns = pending->monotonic_ns;
	event->header.kind = AGENTTAINT_FILE_OPEN;
	event->header.flags = flags;
	bpf_ringbuf_submit(event, 0);
	bpf_map_delete_elem(&pending_file_opens, &tid);
}
