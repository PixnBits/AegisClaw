package main

import (
	"fmt"

	"github.com/seccomp/libseccomp-golang"
	"go.uber.org/zap"
)

// applySeccompFilter installs a strict seccomp-bpf filter.
// This should be called early after capability dropping.
func applySeccompFilter(logger *zap.Logger) error {
	// Create a new filter with default action: kill process on violation
	filter, err := seccomp.NewFilter(seccomp.ActKillProcess)
	if err != nil {
		return fmt.Errorf("failed to create seccomp filter: %w", err)
	}

	// Allowlist of syscalls the daemon is expected to need.
	// This is intentionally conservative.
	allowedSyscalls := []string{
		"accept", "accept4", "access", "arch_prctl", "bind", "brk", "capget", "capset",
		"chdir", "chmod", "chown", "clock_gettime", "clone", "close", "connect", "creat",
		"dup", "dup2", "dup3", "epoll_create", "epoll_create1", "epoll_ctl", "epoll_pwait", "epoll_wait",
		"eventfd", "eventfd2", "execve", "execveat", "exit", "exit_group", "faccessat", "fadvise64",
		"fallocate", "fchdir", "fchmod", "fchmodat", "fchown", "fchownat", "fcntl", "fdatasync",
		"fgetxattr", "flistxattr", "flock", "fork", "fremovexattr", "fsetxattr", "fstat", "fstatat", "fstatfs",
		"fsync", "ftruncate", "futex", "getcwd", "getdents", "getdents64", "getegid", "geteuid",
		"getgid", "getgroups", "getpeername", "getpgid", "getpgrp", "getpid", "getppid", "getpriority",
		"getrandom", "getresgid", "getresuid", "getrlimit", "getrusage", "getsid", "getsockname", "getsockopt",
		"gettid", "gettimeofday", "getuid", "getxattr", "inotify_add_watch", "inotify_init", "inotify_init1",
		"inotify_rm_watch", "ioctl", "ioprio_get", "ioprio_set", "kill", "lchown", "lgetxattr", "link",
		"linkat", "listen", "listxattr", "llistxattr", "lremovexattr", "lseek", "lsetxattr", "lstat",
		"madvise", "mbind", "membarrier", "memfd_create", "migrate_pages", "mincore", "mkdir", "mkdirat",
		"mknod", "mknodat", "mlock", "mlock2", "mlockall", "mmap", "modify_ldt", "mount", "move_pages",
		"mprotect", "mq_getsetattr", "mq_notify", "mq_open", "mq_timedreceive", "mq_timedsend", "mq_unlink",
		"mremap", "msgctl", "msgget", "msgrcv", "msgsnd", "msync", "munlock", "munlockall", "munmap",
		"name_to_handle_at", "nanosleep", "newfstatat", "open", "openat", "openat2", "pause", "perf_event_open",
		"personality", "pipe", "pipe2", "pivot_root", "pkey_alloc", "pkey_free", "pkey_mprotect", "poll",
		"ppoll", "prctl", "pread64", "preadv", "preadv2", "prlimit64", "process_vm_readv", "process_vm_writev",
		"pselect6", "ptrace", "pwrite64", "pwritev", "pwritev2", "read", "readahead", "readlink", "readlinkat",
		"readv", "reboot", "recvfrom", "recvmmsg", "recvmsg", "remap_file_pages", "removexattr", "rename",
		"renameat", "renameat2", "rmdir", "rseq", "rt_sigaction", "rt_sigpending", "rt_sigprocmask",
		"rt_sigqueueinfo", "rt_sigreturn", "rt_sigsuspend", "rt_sigtimedwait", "rt_tgsigqueueinfo", "sched_get_priority_max",
		"sched_get_priority_min", "sched_getaffinity", "sched_getattr", "sched_getparam", "sched_getscheduler",
		"sched_rr_get_interval", "sched_setaffinity", "sched_setattr", "sched_setparam", "sched_setscheduler",
		"sched_yield", "seccomp", "select", "semctl", "semget", "semop", "semtimedop", "sendfile", "sendmmsg",
		"sendmsg", "sendto", "set_robust_list", "set_tid_address", "setdomainname", "setfsgid", "setfsuid",
		"setgid", "setgroups", "sethostname", "setitimer", "setns", "setpgid", "setpriority", "setregid",
		"setresgid", "setresuid", "setreuid", "setrlimit", "setsid", "setsockopt", "settimeofday", "setuid",
		"setxattr", "shmat", "shmctl", "shmdt", "shmget", "shutdown", "sigaltstack", "signalfd", "signalfd4",
		"socket", "socketpair", "splice", "stat", "statfs", "statx", "symlink", "symlinkat", "sync", "sync_file_range",
		"syncfs", "sysinfo", "syslog", "tee", "tgkill", "time", "timer_create", "timer_delete", "timer_getoverrun",
		"timer_gettime", "timer_settime", "timerfd_create", "timerfd_gettime", "timerfd_settime", "times", "tkill",
		"truncate", "umask", "umount2", "uname", "unlink", "unlinkat", "unshare", "userfaultfd", "ustat", "utime",
		"utimensat", "utimes", "vfork", "vhangup", "vmsplice", "wait4", "waitid", "waitpid", "write", "writev",
	}

	for _, name := range allowedSyscalls {
		syscallID, err := seccomp.GetSyscallFromName(name)
		if err != nil {
			// Some syscalls may not exist on all architectures — ignore
			continue
		}
		if err := filter.AddRule(syscallID, seccomp.ActAllow); err != nil {
			return fmt.Errorf("failed to add rule for %s: %w", name, err)
		}
	}

	if err := filter.Load(); err != nil {
		return fmt.Errorf("failed to load seccomp filter: %w", err)
	}

	logger.Info("seccomp-bpf filter applied successfully")
	return nil
}
