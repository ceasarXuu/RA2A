//go:build windows

package codexcli

import (
	"fmt"
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
