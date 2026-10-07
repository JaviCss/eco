//go:build windows

package httpdoor_test

import (
	"os"
	"os/user"
	"path/filepath"
	"strings"
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

func TestPortFileDACLIsAppliedBeforeTheFirstWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "eco.port")
	want := currentUserSID(t)
	observed := make(chan []string, 1)
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			matches, err := filepath.Glob(filepath.Join(dir, "eco.port.*.tmp"))
			if err != nil || len(matches) == 0 {
				continue
			}
			acls, err := httpdoor.PortFileACL(matches[0])
			if err != nil {
				continue
			}
			select {
			case observed <- acls:
			default:
			}
			return
		}
	}()
	info := httpdoor.PortInfo{PID: 4321, Port: 45678, Nonce: "n", Token: strings.Repeat("t", 8*1024*1024)}
	writeErr := httpdoor.WritePortFile(path, info)
	close(stop)
	if writeErr != nil {
		t.Fatalf("WritePortFile: %v", writeErr)
	}
	var first []string
	select {
	case first = <-observed:
	default:
		t.Fatalf("the temporary was never observed while it existed, so the test proves nothing")
	}
	if len(first) != 1 || first[0] != want {
		t.Fatalf("while the token was being written the temporary carried %v, want exactly [%s]", first, want)
	}
	if _, err := httpdoor.PortFileACL(path); err != nil {
		t.Fatalf("PortFileACL of the final port file: %v", err)
	}
	t.Logf("TEMPORARY_ACL_WHILE_WRITING=%v", first)
}
