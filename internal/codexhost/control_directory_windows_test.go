//go:build windows

package codexhost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestControlDirectoryCreatesPrivateDACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codex-home", "app-server-control")
	if err := prepareControlDirectory(path); err != nil {
		t.Fatal(err)
	}
	assertPrivateControlDirectory(t, path)
	before := directorySecurity(t, path).String()
	// Official Codex holds this kind of handle, denying deletion sharing.
	handle := openDirectory(t, path, windows.READ_CONTROL|windows.FILE_READ_ATTRIBUTES|windows.FILE_LIST_DIRECTORY)
	defer windows.CloseHandle(handle)
	if err := prepareControlDirectory(path); err != nil {
		t.Fatalf("prepare while official daemon pins private directory: %v", err)
	}
	if after := directorySecurity(t, path).String(); after != before {
		t.Fatalf("private directory security changed: %s -> %s", before, after)
	}
}

func TestControlDirectoryRepairsInheritedDACLWithoutTouchingChildren(t *testing.T) {
	parent := t.TempDir()
	sid := currentTestSID(t)
	setTestDACL(t, parent, "D:P(A;OICI;FA;;;"+sid.String()+")(A;OICI;FA;;;WD)")
	path := filepath.Join(parent, "app-server-control")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(path, "official.sock")
	sibling := filepath.Join(parent, "auth.json")
	for _, file := range []string{child, sibling} {
		if err := os.WriteFile(file, []byte("retained data"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if privateControlDACL(directorySecurity(t, path), sid) {
		t.Fatal("inherited user + Everyone DACL unexpectedly meets Codex contract")
	}
	ownerBefore, _, err := directorySecurity(t, path).Owner()
	if err != nil {
		t.Fatal(err)
	}
	before := map[string]string{}
	for _, item := range []string{parent, child, sibling} {
		before[item] = directorySecurity(t, item).String()
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := prepareControlDirectory(path); err != nil {
			t.Fatalf("prepare attempt %d: %v", attempt, err)
		}
		assertPrivateControlDirectory(t, path)
	}
	ownerAfter, _, err := directorySecurity(t, path).Owner()
	if err != nil || !ownerBefore.Equals(ownerAfter) {
		t.Fatalf("control directory owner changed: %v", err)
	}
	for item, security := range before {
		if after := directorySecurity(t, item).String(); after != security {
			t.Errorf("security outside target directory changed for %s: %s -> %s", item, security, after)
		}
	}
	for _, file := range []string{child, sibling} {
		if data, err := os.ReadFile(file); err != nil || string(data) != "retained data" {
			t.Errorf("existing data changed for %s: %q, %v", file, data, err)
		}
	}
}

func TestControlDirectoryRejectsNonDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(path, []byte("retained data"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := directorySecurity(t, path).String()
	if err := prepareControlDirectory(path); err == nil {
		t.Fatal("ordinary file accepted as a control directory")
	}
	if after := directorySecurity(t, path).String(); after != before {
		t.Fatal("non-directory ACL changed")
	}
}

func TestControlDirectoryRejectsReparsePoint(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(filepath.Dir(target), "control-link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("directory symlink fixture unavailable: %v", err)
	}
	before := directorySecurity(t, target).String()
	if err := prepareControlDirectory(link); err == nil || !strings.Contains(err.Error(), "untrusted control directory") {
		t.Fatalf("reparse directory not rejected: %v", err)
	}
	if after := directorySecurity(t, target).String(); after != before {
		t.Fatal("reparse target security changed")
	}
}

func TestControlDirectoryRejectsMismatchedOwner(t *testing.T) {
	path := t.TempDir()
	handle := openDirectory(t, path, windows.READ_CONTROL|windows.FILE_READ_ATTRIBUTES|windows.FILE_LIST_DIRECTORY)
	defer windows.CloseHandle(handle)
	other, err := windows.StringToSid("S-1-1-0")
	if err != nil {
		t.Fatal(err)
	}
	before := directorySecurity(t, path).String()
	if _, err := ownedControlDirectory(handle, other); err == nil || !strings.Contains(err.Error(), "not owned") {
		t.Fatalf("foreign owner accepted: %v", err)
	}
	if after := directorySecurity(t, path).String(); after != before {
		t.Fatal("foreign owner rejection changed directory security")
	}
}

func currentTestSID(t *testing.T) *windows.SID {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	return user.User.Sid
}

func directorySecurity(t *testing.T, path string) *windows.SECURITY_DESCRIPTOR {
	t.Helper()
	security, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	return security
}

func assertPrivateControlDirectory(t *testing.T, path string) {
	t.Helper()
	sid := currentTestSID(t)
	security := directorySecurity(t, path)
	owner, _, err := security.Owner()
	if err != nil || owner == nil || !owner.Equals(sid) || !privateControlDACL(security, sid) {
		t.Fatalf("directory does not meet official Codex security contract: %s, %v", security, err)
	}
}

func setTestDACL(t *testing.T, path, sddl string) {
	t.Helper()
	security, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := security.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
}

func openDirectory(t *testing.T, path string, access uint32) windows.Handle {
	t.Helper()
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		t.Fatal(err)
	}
	return handle
}
