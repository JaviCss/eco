//go:build windows

package httpdoor

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows"
)

func currentUserSID() (string, error) {
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("httpdoor: the user SID is unreadable: %w", err)
	}
	if user.User.Sid == nil {
		return "", fmt.Errorf("httpdoor: the token carries no user SID")
	}
	sid, err := user.User.Sid.String(), error(nil)
	if err != nil {
		return "", err
	}
	return sid, nil
}

func restrictFileToOwner(path string) error {
	sid, err := currentUserSID()
	if err != nil {
		return err
	}
	sddl := "D:P(A;;FA;;;" + sid + ")"
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return fmt.Errorf("httpdoor: the owner DACL %q could not be built: %w", sddl, err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("httpdoor: the owner DACL could not be read: %w", err)
	}
	info := uint32(windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION)
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.SECURITY_INFORMATION(info), nil, nil, dacl, nil); err != nil {
		return fmt.Errorf("httpdoor: the DACL could not be applied: %w", err)
	}
	return nil
}

func PortFileACL(path string) ([]string, error) {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return nil, fmt.Errorf("httpdoor: the DACL of the port file is unreadable: %w", err)
	}
	return sidsFromSDDL(sd.String()), nil
}

func sidsFromSDDL(sddl string) []string {
	start := strings.Index(sddl, "D:")
	if start < 0 {
		return nil
	}
	body := sddl[start+2:]
	if idx := strings.Index(body, "O:"); idx >= 0 {
		if g := strings.Index(body, "G:"); g >= idx {
			body = body[:g]
		} else {
			body = body[:idx]
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, ace := range strings.Split(body, "(") {
		ace = strings.TrimSpace(ace)
		if !strings.HasPrefix(ace, "A") {
			continue
		}
		ace = strings.TrimSuffix(ace, ")")
		fields := strings.Split(ace, ";")
		if len(fields) < 4 {
			continue
		}
		sid := strings.TrimSpace(fields[len(fields)-1])
		if sid == "" || seen[sid] {
			continue
		}
		seen[sid] = true
		out = append(out, sid)
	}
	return out
}
