#include "vmlinux.h"
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_endian.h>
#include <bpf/bpf_helpers.h>

#include "events.h"
#include "maps.h"

#define AGENTTAINT_AF_INET 2
#define AGENTTAINT_EINTR 4
#define AGENTTAINT_EALREADY 114
#define AGENTTAINT_EINPROGRESS 115

struct agenttaint_sockaddr_in {
	__u16 family;
	__u16 port;
	__u8 address[4];
	__u8 zero[8];
};
_Static_assert(sizeof(struct agenttaint_sockaddr_in) == 16, "sockaddr_in ABI");

static __always_inline void network_count_drop(__u32 reason)
{
	__u64 *count = bpf_map_lookup_elem(&process_drops, &reason);
	if (count)
		__sync_fetch_and_add(count, 1);
}

static __always_inline int network_read_process(struct task_struct *task,
							struct agenttaint_raw_process *out,
							__u64 scope_id, __u64 birth_id)
{
	struct signal_struct *signal = BPF_CORE_READ(task, signal);
	struct task_struct *parent = BPF_CORE_READ(task, real_parent);
	if (!signal || !parent)
		return -1;
	struct pid *pgrp = BPF_CORE_READ(signal, pids[2]);
	struct pid *session = BPF_CORE_READ(signal, pids[3]);
	if (!pgrp || !session)
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

static __always_inline int network_current_process(struct agenttaint_raw_process *out)
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
	return network_read_process(task, out, scope ? *scope : 0, birth_id);
}

static __always_inline void network_enter_connect(const void *user_address, __u64 address_length)
{
	__u32 tid = (__u32)bpf_get_current_pid_tgid();
	if (!tid)
		return;
	bpf_map_delete_elem(&pending_connects, &tid);
	if (address_length < sizeof(struct agenttaint_sockaddr_in)) {
		network_count_drop(AGENTTAINT_DROP_NETWORK_CAPTURE);
		return;
	}
	struct agenttaint_sockaddr_in input = {};
	if (bpf_probe_read_user(&input, sizeof(input), user_address) < 0) {
		network_count_drop(AGENTTAINT_DROP_NETWORK_CAPTURE);
		return;
	}
	if (input.family != AGENTTAINT_AF_INET)
		return;

	struct agenttaint_pending_connect pending = {};
	int process_status = network_current_process(&pending.process);
	if (process_status == -2)
		return;
	if (process_status < 0) {
		network_count_drop(AGENTTAINT_DROP_NETWORK_CAPTURE);
		return;
	}
	pending.monotonic_ns = bpf_ktime_get_ns();
	pending.port = bpf_ntohs(input.port);
	__builtin_memcpy(pending.address, input.address, sizeof(pending.address));
	if (bpf_map_update_elem(&pending_connects, &tid, &pending, BPF_ANY) < 0)
		network_count_drop(AGENTTAINT_DROP_NETWORK_CAPTURE);
}

static __always_inline void network_exit_connect(long result)
{
	__u32 tid = (__u32)bpf_get_current_pid_tgid();
	struct agenttaint_pending_connect *pending = bpf_map_lookup_elem(&pending_connects, &tid);
	if (!pending)
		return;
	struct agenttaint_raw_network_connect *event = bpf_ringbuf_reserve(&process_events, sizeof(*event), 0);
	if (!event) {
		bpf_map_delete_elem(&pending_connects, &tid);
		network_count_drop(AGENTTAINT_DROP_NETWORK_RING);
		return;
	}
	__builtin_memset(event, 0, sizeof(*event));
	event->process = pending->process;
	__builtin_memcpy(event->address, pending->address, sizeof(event->address));
	event->port = pending->port;
	__u8 flags = 0;
	if (result == -AGENTTAINT_EINTR || result == -AGENTTAINT_EALREADY || result == -AGENTTAINT_EINPROGRESS)
		flags |= AGENTTAINT_OPERATION_UNKNOWN;
	else if (result < 0)
		flags |= AGENTTAINT_OPERATION_FAILED;
	event->header.magic = AGENTTAINT_RAW_MAGIC;
	event->header.version = AGENTTAINT_RAW_VERSION;
	event->header.length = sizeof(*event);
	event->header.monotonic_ns = pending->monotonic_ns;
	event->header.kind = AGENTTAINT_NETWORK_CONNECT;
	event->header.flags = flags;
	bpf_ringbuf_submit(event, 0);
	bpf_map_delete_elem(&pending_connects, &tid);
}
