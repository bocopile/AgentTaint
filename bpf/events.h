#ifndef AGENTTAINT_BPF_EVENTS_H
#define AGENTTAINT_BPF_EVENTS_H

#define AGENTTAINT_RAW_MAGIC 0x41544745U
#define AGENTTAINT_RAW_VERSION 1U
#define AGENTTAINT_COMM_LEN 16U
#define AGENTTAINT_EXEC_PATH_LEN 256U
#define AGENTTAINT_PENDING_PATH_CAPTURE_FAILED (1U << 7)

enum agenttaint_raw_kind {
	AGENTTAINT_PROCESS_FORK = 1,
	AGENTTAINT_PROCESS_EXEC = 2,
	AGENTTAINT_PROCESS_EXIT = 3,
	AGENTTAINT_FILE_OPEN = 4,
	AGENTTAINT_NETWORK_CONNECT = 5,
};

enum agenttaint_raw_flags {
	AGENTTAINT_PATH_TRUNCATED = 1U << 0,
	AGENTTAINT_PATH_RELATIVE = 1U << 1,
	AGENTTAINT_OPERATION_FAILED = 1U << 2,
	AGENTTAINT_OPERATION_UNKNOWN = 1U << 3,
};

enum agenttaint_file_access {
	AGENTTAINT_ACCESS_UNKNOWN = 0,
	AGENTTAINT_ACCESS_READ = 1,
	AGENTTAINT_ACCESS_WRITE = 2,
	AGENTTAINT_ACCESS_READ_WRITE = 3,
};

enum agenttaint_drop_reason {
	AGENTTAINT_DROP_PROCESS_RING = 0,
	AGENTTAINT_DROP_PROCESS_CAPTURE = 1,
	AGENTTAINT_DROP_FILE_RING = 2,
	AGENTTAINT_DROP_FILE_CAPTURE = 3,
	AGENTTAINT_DROP_NETWORK_RING = 4,
	AGENTTAINT_DROP_NETWORK_CAPTURE = 5,
	AGENTTAINT_DROP_FILE_SYSCALL_ARGS = 6,
	AGENTTAINT_DROP_FILE_PROCESS = 7,
	AGENTTAINT_DROP_FILE_PENDING = 8,
	AGENTTAINT_DROP_FILE_PATH_SUCCESS = 9,
	AGENTTAINT_DROP_FILE_PATH_OTHER_ERROR = 10,
	AGENTTAINT_DROP_REASON_COUNT = 11,
};

/* Fixed-width native-endian ABI. Every field has an explicit offset. */
struct agenttaint_raw_header {
	__u32 magic;
	__u16 version;
	__u16 length;
	__u64 monotonic_ns;
	__u8 kind;
	__u8 flags;
	__u16 reserved0;
	__u32 reserved1;
};

struct agenttaint_raw_process {
	__u32 pid;
	__u32 ppid;
	__u32 pgid;
	__u32 session_id;
	__u64 birth_id;
	__u64 scope_id;
	char comm[AGENTTAINT_COMM_LEN];
};

struct agenttaint_raw_fork {
	struct agenttaint_raw_header header;
	struct agenttaint_raw_process parent;
	struct agenttaint_raw_process child;
};

struct agenttaint_raw_exec {
	struct agenttaint_raw_header header;
	struct agenttaint_raw_process process;
	char path[AGENTTAINT_EXEC_PATH_LEN];
};

struct agenttaint_raw_exit {
	struct agenttaint_raw_header header;
	struct agenttaint_raw_process process;
};

struct agenttaint_raw_file_open {
	struct agenttaint_raw_header header;
	struct agenttaint_raw_process process;
	char path[AGENTTAINT_EXEC_PATH_LEN];
	__u8 access;
	__u8 reserved[7];
};

struct agenttaint_raw_network_connect {
	struct agenttaint_raw_header header;
	struct agenttaint_raw_process process;
	__u8 address[4];
	__u16 port;
	__u16 reserved;
};

struct agenttaint_pending_file_open {
	__u64 monotonic_ns;
	struct agenttaint_raw_process process;
	char path[AGENTTAINT_EXEC_PATH_LEN];
	__u8 flags;
	__u8 access;
	__u8 reserved[6];
};

struct agenttaint_pending_connect {
	__u64 monotonic_ns;
	struct agenttaint_raw_process process;
	__u8 address[4];
	__u16 port;
	__u16 reserved;
};

_Static_assert(sizeof(struct agenttaint_raw_header) == 24, "raw header ABI");
_Static_assert(sizeof(struct agenttaint_raw_process) == 48, "raw process ABI");
_Static_assert(sizeof(struct agenttaint_raw_fork) == 120, "raw fork ABI");
_Static_assert(sizeof(struct agenttaint_raw_exec) == 328, "raw exec ABI");
_Static_assert(sizeof(struct agenttaint_raw_exit) == 72, "raw exit ABI");
_Static_assert(sizeof(struct agenttaint_raw_file_open) == 336, "raw file-open ABI");
_Static_assert(sizeof(struct agenttaint_raw_network_connect) == 80, "raw network-connect ABI");

#endif
