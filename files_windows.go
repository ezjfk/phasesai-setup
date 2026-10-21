//go:build windows

package main

import (
	"errors"
	"golang.org/x/sys/windows"
	"os"
	"syscall"
)

var reopenFile = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReOpenFile")

func isReparse(info os.FileInfo) bool {
	if data, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		return data.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
	}
	return info.Mode()&os.ModeSymlink != 0
}
func inspectFile(f *os.File) (fileMetadata, error) {
	handle := windows.Handle(f.Fd())
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return fileMetadata{}, err
	}
	if info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 || info.NumberOfLinks != 1 {
		return fileMetadata{}, errors.New("reparse points, directories and hard-linked config files are refused")
	}
	sd, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fileMetadata{}, err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return fileMetadata{}, err
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fileMetadata{}, err
	}
	if !owner.Equals(user.User.Sid) {
		return fileMetadata{}, errors.New("config file must be owned by your Windows account")
	}
	return fileMetadata{Mode: 0600, Security: sd.String()}, nil
}
func securityHandle(f *os.File) (windows.Handle, error) {
	handle, _, err := reopenFile.Call(f.Fd(), uintptr(windows.READ_CONTROL|windows.WRITE_DAC), uintptr(windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE), 0)
	if handle == uintptr(windows.InvalidHandle) {
		return windows.InvalidHandle, err
	}
	return windows.Handle(handle), nil
}
func setFileSecurity(f *os.File, sd *windows.SECURITY_DESCRIPTOR) error {
	acl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	handle, err := securityHandle(f)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	control, _, err := sd.Control()
	if err != nil {
		return err
	}
	protection := windows.SECURITY_INFORMATION(windows.UNPROTECTED_DACL_SECURITY_INFORMATION)
	if control&windows.SE_DACL_PROTECTED != 0 {
		protection = windows.PROTECTED_DACL_SECURITY_INFORMATION
	}
	return windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|protection, nil, nil, acl, nil)
}
func secureFile(f *os.File) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.User.Sid.String() + ")(A;;FA;;;SY)")
	if err != nil {
		return err
	}
	return setFileSecurity(f, sd)
}
func restoreMetadata(f *os.File, m fileMetadata) error {
	if m.Security == "" {
		return secureFile(f)
	}
	sd, err := windows.SecurityDescriptorFromString(m.Security)
	if err != nil {
		return err
	}
	return setFileSecurity(f, sd)
}
func lockFile(f *os.File) error {
	var overlap windows.Overlapped
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlap)
}
func syncDirectory(root *os.Root) error {
	// Windows does not expose an unprivileged portable directory fsync. Every
	// data/journal file is flushed; os.Root.Rename uses handle-relative native
	// rename. Journals support interrupted-process recovery, not a guarantee for
	// power loss on every filesystem/device.
	return nil
}
