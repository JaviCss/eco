//go:build windows

package httpdoor_test

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"

	"github.com/JaviCss/eco/httpdoor"
	"golang.org/x/sys/windows"
)

func currentUserSID(t *testing.T) string {
	t.Helper()
	current, err := user.Current()
	if err != nil {
		t.Fatalf("user.Current: %v", err)
	}
	account, _, _, err := windows.LookupSID("", current.Username)
	if err != nil {
		t.Fatalf("LookupSID %q: %v", current.Username, err)
	}
	sid := account.String()
	t.Logf("CURRENT_USER=%s SID=%s", current.Username, sid)
	return sid
}

func TestPortFileDACLIsOnlyTheCurrentUser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "eco.port")
	info := httpdoor.PortInfo{PID: 4321, Port: 45678, Nonce: "n", Token: "t"}
	if err := httpdoor.WritePortFile(path, info); err != nil {
		t.Fatalf("WritePortFile: %v", err)
	}
	want := currentUserSID(t)
	acls, err := httpdoor.PortFileACL(path)
	if err != nil {
		t.Fatalf("PortFileACL: %v", err)
	}
	if len(acls) != 1 || acls[0] != want {
		t.Fatalf("the DACL is %v, want exactly [%s]", acls, want)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the port file is gone: %v", err)
	}
}
