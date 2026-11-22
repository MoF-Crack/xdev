//go:build windows

package signer

import (
	"golang.org/x/sys/windows"
	"path/filepath"
	"testing"
	"unsafe"
)

func TestKeygenPrivateProtectedDACL(t *testing.T) {
	d := t.TempDir()
	s := filepath.Join(d, "secret")
	if e := GenerateKeyPair(filepath.Join(d, "public"), s); e != nil {
		t.Fatal(e)
	}
	sd, e := windows.GetNamedSecurityInfo(s, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if e != nil || sd == nil {
		t.Fatal("no private file security descriptor", e)
	}
	ctrl, _, e := sd.Control()
	if e != nil || ctrl&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("inheritable DACL", e)
	}
	acl, _, e := sd.DACL()
	if e != nil || acl == nil || acl.AceCount != 1 {
		t.Fatal("expected exactly one ACE", e)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if e = windows.GetAce(acl, 0, &ace); e != nil {
		t.Fatal(e)
	}
	user, e := windows.GetCurrentProcessToken().GetTokenUser()
	if e != nil {
		t.Fatal(e)
	}
	if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || !((*windows.SID)(unsafe.Pointer(&ace.SidStart))).Equals(user.User.Sid) {
		t.Fatal("private DACL does not exclusively grant current user")
	}
}
