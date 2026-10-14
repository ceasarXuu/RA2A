//go:build linux

package codexowner

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// ReadWriter returns nil only when no writer was observed. Read/identity errors
// remain errors. The process and file must be in this reader's PID/mount view.
func ReadWriter(ctx context.Context, lockPath string) (*Holder, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := statLock(lockPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	pid, err := lockPID(ctx, file)
	if err != nil {
		return nil, err
	}
	if pid == 0 {
		return nil, nil
	}
	start, exe, err := processIdentity(pid)
	if err != nil {
		return nil, fmt.Errorf("read holder %d identity: %w", pid, err)
	}
	current, err := statLock(lockPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrChanged, err)
	}
	if file.Dev != current.Dev || file.Ino != current.Ino {
		return nil, ErrChanged
	}
	currentPID, err := lockPID(ctx, current)
	if err != nil {
		return nil, err
	}
	currentStart, currentExe, err := processIdentity(pid)
	if err != nil || currentPID != pid || currentStart != start || currentExe != exe {
		return nil, ErrChanged
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &Holder{PID: pid, StartTicks: start, Executable: exe, Device: uint64(file.Dev), Inode: file.Ino}, nil
}

func statLock(path string) (*syscall.Stat_t, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("writer lock is not a regular file: %s", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, ErrUnsupported
	}
	return stat, nil
}

func lockPID(ctx context.Context, file *syscall.Stat_t) (int, error) {
	f, err := os.Open("/proc/locks")
	if err != nil {
		return 0, fmt.Errorf("read native lock table: %w", err)
	}
	defer f.Close()
	key := fmt.Sprintf("%x:%x:%d", unix.Major(uint64(file.Dev)), unix.Minor(uint64(file.Dev)), file.Ino)
	scanner := bufio.NewScanner(f)
	pid := 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		owner, relevant, err := parseLock(scanner.Text(), key)
		if err != nil {
			return 0, err
		}
		if relevant {
			if pid != 0 {
				return 0, fmt.Errorf("ambiguous locks for %s", key)
			}
			pid = owner
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("read native lock table: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return pid, nil
}

func parseLock(line, key string) (int, bool, error) {
	fields := strings.Fields(line)
	// Waiting records are not holders. Kernel device fields may be zero-padded.
	if len(fields) < 8 || fields[1] == "->" {
		return 0, false, nil
	}
	device := strings.Split(fields[5], ":")
	if len(device) != 3 {
		return 0, false, nil
	}
	major, e1 := strconv.ParseUint(device[0], 16, 32)
	minor, e2 := strconv.ParseUint(device[1], 16, 32)
	inode, e3 := strconv.ParseUint(device[2], 10, 64)
	if e1 != nil || e2 != nil || e3 != nil || fmt.Sprintf("%x:%x:%d", major, minor, inode) != key {
		return 0, false, nil
	}
	if fields[1] != "FLOCK" || fields[2] != "ADVISORY" || fields[3] != "WRITE" || fields[6] != "0" || fields[7] != "EOF" {
		return 0, true, fmt.Errorf("unsupported native writer lock: %s", line)
	}
	pid, err := strconv.Atoi(fields[4])
	if err != nil || pid <= 0 {
		return 0, true, fmt.Errorf("native lock has no identifiable process: %s", line)
	}
	return pid, true, nil
}

func processIdentity(pid int) (uint64, string, error) {
	root := "/proc/" + strconv.Itoa(pid)
	data, err := os.ReadFile(root + "/stat")
	if err != nil {
		return 0, "", err
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return 0, "", fmt.Errorf("invalid process stat")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 {
		return 0, "", fmt.Errorf("incomplete process stat")
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return 0, "", err
	}
	exe, err := os.Readlink(root + "/exe")
	return start, exe, err
}
