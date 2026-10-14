//go:build windows

package codexhost

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

const controlDirectoryFullAccess = 0x1f01ff // Windows FILE_ALL_ACCESS.

// Match Codex's protected, inheritable user-only control directory contract.
// Only its DACL is repaired: owner, child ACLs and sibling directories are kept.
func prepareControlDirectory(path string) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	sid := user.User.Sid
	descriptor, err := windows.SecurityDescriptorFromString(fmt.Sprintf("O:%sD:P(A;OICI;FA;;;%s)", sid, sid))
	if err != nil {
		return err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return errors.New("control directory must not be a filesystem root")
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	parent, err = filepath.EvalSymlinks(parent)
	if err != nil {
		return err
	}
	name, err := windows.UTF16PtrFromString(filepath.Join(parent, filepath.Base(path)))
	if err != nil {
		return err
	}
	attributes := windows.SecurityAttributes{
		Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: descriptor,
	}
	if err := windows.CreateDirectory(name, &attributes); err == nil {
		return nil
	} else if !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return err
	}
	// A running official daemon pins a private directory against deletion.
	// Inspect without DELETE access first so its guard does not block RA2A.
	for _, access := range []uint32{windows.READ_CONTROL | windows.FILE_READ_ATTRIBUTES | windows.FILE_LIST_DIRECTORY, windows.MAXIMUM_ALLOWED} {
		handle, err := windows.CreateFile(name, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
			windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		if err != nil {
			return err
		}
		security, err := ownedControlDirectory(handle, sid)
		if err != nil {
			windows.CloseHandle(handle)
			return err
		}
		if privateControlDACL(security, sid) {
			windows.CloseHandle(handle)
			return nil
		}
		if access == windows.MAXIMUM_ALLOWED {
			dacl, _, err := descriptor.DACL()
			if err == nil {
				// MAXIMUM_ALLOWED prevents SetSecurityInfo propagating ACEs to
				// existing children (Microsoft's documented API contract).
				err = windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT,
					windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
			}
			windows.CloseHandle(handle)
			return err
		}
		windows.CloseHandle(handle)
	}
	return nil
}

func ownedControlDirectory(handle windows.Handle, sid *windows.SID) (*windows.SECURITY_DESCRIPTOR, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return nil, err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return nil, errors.New("untrusted control directory: not a directory or reparse point")
	}
	security, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return nil, err
	}
	owner, _, err := security.Owner()
	if err != nil {
		return nil, err
	}
	if owner == nil || !owner.Equals(sid) {
		return nil, errors.New("control directory is not owned by the current user")
	}
	return security, nil
}

func privateControlDACL(security *windows.SECURITY_DESCRIPTOR, sid *windows.SID) bool {
	control, _, err := security.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		return false
	}
	dacl, _, err := security.DACL()
	if err != nil || dacl == nil || dacl.AceCount != 1 {
		return false
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil {
		return false
	}
	return ace.Header.AceType == windows.ACCESS_ALLOWED_ACE_TYPE &&
		ace.Header.AceFlags == windows.CONTAINER_INHERIT_ACE|windows.OBJECT_INHERIT_ACE &&
		ace.Mask == controlDirectoryFullAccess && (*windows.SID)(unsafe.Pointer(&ace.SidStart)).Equals(sid)
}
