//go:build windows

package securefs

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func EnsurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return setPrivateACL(path, true)
}

func RestrictPrivateFile(path string) error {
	return setPrivateACL(path, false)
}

func setPrivateACL(path string, directory bool) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Errorf("resolve current Windows user: %w", err)
	}
	userSID := user.User.Sid.String()
	flags := ""
	if directory {
		flags = "OICI"
	}
	sddl := fmt.Sprintf("D:P(A;%s;FA;;;SY)(A;%s;FA;;;BA)(A;%s;FA;;;%s)", flags, flags, flags, userSID)
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return fmt.Errorf("build private Windows ACL: %w", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("read private Windows ACL: %w", err)
	}
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		dacl,
		nil,
	); err != nil {
		return fmt.Errorf("apply private Windows ACL: %w", err)
	}
	return nil
}
