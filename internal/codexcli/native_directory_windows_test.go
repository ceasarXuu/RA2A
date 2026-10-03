//go:build windows

package codexcli

import (
	"fmt"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Match rust-v0.160.0 uds/windows_security.rs's private-directory contract.
// Only create a fresh child of the owned temporary home. Existing directories
// are errors; this fixture never repairs an ACL or follows a production path.
func createNativeStateDirectory(path string) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	sid := user.User.Sid
	descriptor, err := windows.SecurityDescriptorFromString(fmt.Sprintf("O:%sD:P(A;OICI;FA;;;%s)", sid, sid))
	if err != nil {
		return err
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	attributes := windows.SecurityAttributes{
		Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: descriptor,
	}
	return windows.CreateDirectory(name, &attributes)
}

// Go's EvalSymlinks failed for the native installer's valid current junction
// on Windows. Resolve the opened object through the same read-only Win32 API
// that confirmed the retained fixture's junction and release were equivalent.
func canonicalNativePath(path string) (string, error) {
	name, err := windows.UTF16PtrFromString(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	handle, err := windows.CreateFile(name, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(handle)
	buffer := make([]uint16, 512)
	for {
		n, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), 0)
		if err != nil {
			return "", err
		}
		if n < uint32(len(buffer)) {
			resolved := windows.UTF16ToString(buffer[:n])
			if strings.HasPrefix(resolved, `\\?\UNC\`) {
				resolved = `\\` + resolved[len(`\\?\UNC\`):]
			} else {
				resolved = strings.TrimPrefix(resolved, `\\?\`)
			}
			return filepath.Clean(resolved), nil
		}
		buffer = make([]uint16, n+1)
	}
}

func verifyNativeProcess(pid int, recordedStart, executable string) error {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &created, &exited, &kernel, &user); err != nil {
		return err
	}
	start := fmt.Sprint(uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime))
	if start != recordedStart {
		return fmt.Errorf("native PID %d creation time %s differs from record %s", pid, start, recordedStart)
	}
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size); err != nil {
		return err
	}
	actual, err := canonicalNativePath(windows.UTF16ToString(buffer[:size]))
	if err != nil || !nativePathEqual(actual, executable) {
		return fmt.Errorf("native PID %d executable %s differs from managed executable %s: %v", pid, actual, executable, err)
	}
	return nil
}
