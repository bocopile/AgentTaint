#include "vmlinux.h"
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_endian.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>

#define ENFORCE_MAX_SOURCES 64
#define ENFORCE_MAX_DENIES 256
#define ENFORCE_MAX_PROCESSES 16384
#define ENFORCE_PATH_SIZE 256
#define ENFORCE_AF_INET 2
#define ENFORCE_EPERM 1
#define ENFORCE_FAULT_STATE 1

struct enforce_config {
	__u64 cgroup_id;
	__u32 deny_count;
	__u32 reserved;
};

struct enforce_process_key {
	__u32 tgid;
	__u32 reserved;
	__u64 birth_id;
};

struct enforce_process_state {
	struct bpf_spin_lock lock;
	__u32 secret;
};

struct enforce_deny_rule {
	__u32 address;
	__u16 port;
	__u8 prefix_len;
	__u8 any_port;
};

struct enforce_source_value {
	char path[ENFORCE_PATH_SIZE];
};

struct {
	__uint(type, BPF_MAP_TYPE_ARRAY);
	__uint(max_entries, 1);
	__type(key, __u32);
	__type(value, struct enforce_config);
} enforce_config_map SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_ARRAY);
	__uint(max_entries, 1);
	__type(key, __u32);
	__type(value, __u64);
} enforce_fault_map SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, ENFORCE_MAX_SOURCES);
	__type(key, __u64);
	__type(value, struct enforce_source_value);
} enforce_sources SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, ENFORCE_MAX_SOURCES);
	__type(key, __u64);
	__type(value, struct enforce_source_value);
} enforce_file_denies SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_ARRAY);
	__uint(max_entries, ENFORCE_MAX_DENIES);
	__type(key, __u32);
	__type(value, struct enforce_deny_rule);
} enforce_denies SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, ENFORCE_MAX_PROCESSES);
	__type(key, struct enforce_process_key);
	__type(value, struct enforce_process_state);
} enforce_processes SEC(".maps");

static __always_inline struct enforce_config *get_config(void)
{
	__u32 zero = 0;
	return bpf_map_lookup_elem(&enforce_config_map, &zero);
}

static __always_inline void set_fault(void)
{
	__u32 zero = 0;
	__u64 *fault = bpf_map_lookup_elem(&enforce_fault_map, &zero);
	if (fault)
		__sync_fetch_and_or(fault, ENFORCE_FAULT_STATE);
}

static __always_inline int process_key(struct task_struct *task,
					      struct enforce_process_key *key)
{
	struct task_struct *leader;
	if (!task)
		return -1;
	leader = BPF_CORE_READ(task, group_leader);
	if (!leader)
		return -1;
	key->tgid = BPF_CORE_READ(task, tgid);
	key->reserved = 0;
	key->birth_id = BPF_CORE_READ(leader, start_boottime);
	return key->tgid && key->birth_id ? 0 : -1;
}

static __always_inline int in_scope(struct enforce_config *cfg)
{
	return cfg && cfg->cgroup_id && bpf_get_current_cgroup_id() == cfg->cgroup_id;
}

static __always_inline void fail_scope(void)
{
	set_fault();
}

static __always_inline __u64 source_path_hash(const char *path)
{
	__u64 hash = 14695981039346656037ULL;
	__u32 i;

#pragma unroll
	for (i = 0; i < ENFORCE_PATH_SIZE; i++) {
		__u8 value = path[i];
		if (!value)
			break;
		hash ^= value;
		hash *= 1099511628211ULL;
	}
	return hash;
}

static __always_inline int source_path_matches(const char *path)
{
	__u64 hash = source_path_hash(path);
	struct enforce_source_value *source = bpf_map_lookup_elem(&enforce_sources, &hash);
	__u32 i;

	if (!source)
		return 0;
#pragma unroll
	for (i = 0; i < ENFORCE_PATH_SIZE; i++) {
		if (path[i] != source->path[i])
			return 0;
		if (!path[i])
			return 1;
	}
	return 0;
}

static __always_inline int file_path_denied(const char *path)
{
	__u64 hash = source_path_hash(path);
	struct enforce_source_value *deny = bpf_map_lookup_elem(&enforce_file_denies, &hash);
	__u32 i;

	if (!deny)
		return 0;
#pragma unroll
	for (i = 0; i < ENFORCE_PATH_SIZE; i++) {
		if (path[i] != deny->path[i])
			return 0;
		if (!path[i])
			return 1;
	}
	return 0;
}

SEC("lsm.s/file_open")
int BPF_PROG(enforce_file_open, struct file *file, int previous)
{
	struct enforce_config *cfg = get_config();
	struct enforce_process_key key = {};
	struct enforce_process_state zero_state = {};
	struct enforce_process_state *state;
	struct task_struct *task;
	char path[ENFORCE_PATH_SIZE] = {};
	long path_len;

	if (previous)
		return previous;
	if (!in_scope(cfg))
		return previous;
	path_len = bpf_d_path((struct path *)&file->f_path, path, sizeof(path));
	if (path_len < 0) {
		fail_scope();
		return -ENFORCE_EPERM;
	}
	if (file_path_denied(path))
		return -ENFORCE_EPERM;
	if (!source_path_matches(path))
		return previous;
	task = bpf_get_current_task_btf();
	if (process_key(task, &key) < 0) {
		fail_scope();
		return -ENFORCE_EPERM;
	}
	state = bpf_map_lookup_elem(&enforce_processes, &key);
	if (!state) {
		if (bpf_map_update_elem(&enforce_processes, &key, &zero_state, BPF_NOEXIST) < 0) {
			state = bpf_map_lookup_elem(&enforce_processes, &key);
			if (!state) {
				fail_scope();
				return -ENFORCE_EPERM;
			}
		} else {
			state = bpf_map_lookup_elem(&enforce_processes, &key);
		}
	}
	if (!state) {
		fail_scope();
		return -ENFORCE_EPERM;
	}
	bpf_spin_lock(&state->lock);
	state->secret = 1;
	bpf_spin_unlock(&state->lock);
	return previous;
}

SEC("lsm/socket_connect")
int BPF_PROG(enforce_socket_connect, struct socket *sock,
	     struct sockaddr *address, int address_len, int previous)
{
	struct enforce_config *cfg = get_config();
	struct enforce_process_key key = {};
	struct enforce_process_state *state;
	struct sockaddr_in *addr = (struct sockaddr_in *)address;
	__u32 zero = 0;
	__u64 *fault;
	__u32 host_address;
	__u16 host_port;
	__u32 i;

	if (previous)
		return previous;
	if (!in_scope(cfg))
		return previous;
	fault = bpf_map_lookup_elem(&enforce_fault_map, &zero);
	if (!fault || (*fault & ENFORCE_FAULT_STATE))
		return -ENFORCE_EPERM;
	if (!address || address_len < sizeof(*addr) || BPF_CORE_READ(address, sa_family) != ENFORCE_AF_INET)
		return previous;
	if (process_key(bpf_get_current_task_btf(), &key) < 0)
		return -ENFORCE_EPERM;
	state = bpf_map_lookup_elem(&enforce_processes, &key);
	if (!state)
		return previous;
	bpf_spin_lock(&state->lock);
	if (!state->secret) {
		bpf_spin_unlock(&state->lock);
		return previous;
	}
	bpf_spin_unlock(&state->lock);
	host_address = bpf_ntohl(BPF_CORE_READ(addr, sin_addr.s_addr));
	host_port = bpf_ntohs(BPF_CORE_READ(addr, sin_port));
	for (i = 0; i < ENFORCE_MAX_DENIES; i++) {
		struct enforce_deny_rule *rule;
		__u32 mask;
		if (i >= cfg->deny_count)
			break;
		rule = bpf_map_lookup_elem(&enforce_denies, &i);
		if (!rule || rule->prefix_len > 32) {
			fail_scope();
			return -ENFORCE_EPERM;
		}
		mask = rule->prefix_len == 0 ? 0 : 0xffffffffU << (32 - rule->prefix_len);
		if ((!rule->any_port && rule->port != host_port) ||
		    (host_address & mask) != (rule->address & mask))
			continue;
		return -ENFORCE_EPERM;
	}
	return previous;
}

SEC("tp_btf/sched_process_fork")
int BPF_PROG(enforce_process_fork, struct task_struct *parent, struct task_struct *child)
{
	struct enforce_config *cfg = get_config();
	struct enforce_process_key parent_key = {};
	struct enforce_process_key child_key = {};
	struct enforce_process_state child_state = {};
	struct enforce_process_state *parent_state;
	__u32 parent_tgid;

	if (!in_scope(cfg))
		return 0;
	parent_tgid = BPF_CORE_READ(parent, tgid);
	if (process_key(parent, &parent_key) < 0 || process_key(child, &child_key) < 0) {
		fail_scope();
		return 0;
	}
	if (parent_tgid == child_key.tgid)
		return 0;
	parent_state = bpf_map_lookup_elem(&enforce_processes, &parent_key);
	if (!parent_state) {
		fail_scope();
		return 0;
	}
	bpf_spin_lock(&parent_state->lock);
	child_state.secret = parent_state->secret;
	bpf_spin_unlock(&parent_state->lock);
	if (bpf_map_update_elem(&enforce_processes, &child_key, &child_state, BPF_NOEXIST) < 0) {
		struct enforce_process_state *existing = bpf_map_lookup_elem(&enforce_processes, &child_key);
		if (!existing) {
			fail_scope();
			return 0;
		}
		bpf_spin_lock(&existing->lock);
		existing->secret = child_state.secret;
		bpf_spin_unlock(&existing->lock);
	}
	return 0;
}

SEC("tp_btf/sched_process_exec")
int BPF_PROG(enforce_process_exec, struct task_struct *task, pid_t old_pid,
	     struct linux_binprm *bprm)
{
	struct enforce_config *cfg = get_config();
	struct enforce_process_key key = {};
	struct enforce_process_state clean_state = {};
	struct enforce_process_state *existing;

	if (!in_scope(cfg))
		return 0;
	if (process_key(task, &key) < 0) {
		fail_scope();
		return 0;
	}
	existing = bpf_map_lookup_elem(&enforce_processes, &key);
	if (existing)
		return 0;
	if (bpf_map_update_elem(&enforce_processes, &key, &clean_state, BPF_NOEXIST) < 0 &&
	    !bpf_map_lookup_elem(&enforce_processes, &key))
		fail_scope();
	return 0;
}

SEC("tp_btf/sched_process_exit")
int BPF_PROG(enforce_process_exit, struct task_struct *task, bool group_dead)
{
	struct enforce_config *cfg = get_config();
	struct enforce_process_key key = {};
	if (!group_dead || !in_scope(cfg))
		return 0;
	if (process_key(task, &key) < 0) {
		fail_scope();
		return 0;
	}
	bpf_map_delete_elem(&enforce_processes, &key);
	return 0;
}

char LICENSE[] SEC("license") = "GPL";
