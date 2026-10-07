//go:build windows

package httpdoor

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func ownerOnlyAttributes() (*windows.SecurityAttributes, error) {
	sid, err := currentUserSID()
	if err != nil {
		return nil, err
	}
	sddl := "D:P(A;;FA;;;" + sid + ")"
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return nil, fmt.Errorf("httpdoor: the owner DACL %q could not be built: %w", sddl, err)
	}
	return &windows.SecurityAttributes{
		Length:            uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: sd,
	}, nil
}

func createPrivateTemp(dir, prefix, suffix string) (*os.File, string, error) {
	attributes, err := ownerOnlyAttributes()
	if err != nil {
		return nil, "", err
	}
	for attempt := 0; attempt < 8; attempt++ {
		raw := make([]byte, 8)
		if _, err := rand.Read(raw); err != nil {
			return nil, "", fmt.Errorf("httpdoor: %w", err)
		}
		name := filepath.Join(dir, prefix+"."+hex.EncodeToString(raw)+suffix)
		target, err := windows.UTF16PtrFromString(name)
		if err != nil {
			return nil, "", fmt.Errorf("httpdoor: %w", err)
		}
		handle, err := windows.CreateFile(
			target,
			windows.GENERIC_READ|windows.GENERIC_WRITE,
			0,
			attributes,
			windows.CREATE_NEW,
			windows.FILE_ATTRIBUTE_NORMAL,
			0,
		)
		if err != nil {
			if errors.Is(err, windows.ERROR_FILE_EXISTS) {
				continue
			}
			return nil, "", fmt.Errorf("httpdoor: %w", err)
		}
		return os.NewFile(uintptr(handle), name), name, nil
	}
	return nil, "", fmt.Errorf("httpdoor: no free temporary name under %s", dir)
}

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
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("httpdoor: the DACL of %s is unreadable: %w", filepath.Base(path), err)
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
