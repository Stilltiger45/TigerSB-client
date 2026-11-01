package desktop

import (
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Who may touch the service's files, in SDDL: the system and administrators
// everything; the computer's users may read the control file and list the
// service's directory, nothing in data.
const (
	sddlRoot    = "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;;0x1200a9;;;BU)"
	sddlData    = "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"
	sddlControl = "D:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;FR;;;BU)"
)

func setDACL(path, sddl string) error {
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}

// secureDir makes dir and its parent, the service's directory, and gives
// them their access rights.
func secureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := setDACL(filepath.Dir(dir), sddlRoot); err != nil {
		return err
	}
	return setDACL(dir, sddlData)
}

func writeForUsers(path string, b []byte) error {
	os.Remove(path)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return err
	}
	return setDACL(path, sddlControl)
}

const stateKeyFile = "state.key"

// stateKey returns the key that encrypts the saved subscriptions. It is kept
// in dir sealed with DPAPI for the account the service runs as, so the files
// copied elsewhere tell nothing.
func stateKey(dir string) ([]byte, error) {
	path := filepath.Join(dir, stateKeyFile)
	if sealed, err := os.ReadFile(path); err == nil {
		key, err := unprotect(sealed)
		if err == nil && len(key) == 32 {
			return key, nil
		}
		// Sealed by another account or broken: the subscriptions it
		// encrypted cannot be read anyway.
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	sealed, err := protect(key)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, sealed, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

func blob(b []byte) *windows.DataBlob {
	if len(b) == 0 {
		return &windows.DataBlob{}
	}
	return &windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
}

func takeBlob(out *windows.DataBlob) []byte {
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...)
}

func protect(b []byte) ([]byte, error) {
	var out windows.DataBlob
	name, _ := windows.UTF16PtrFromString("TigerSB")
	if err := windows.CryptProtectData(blob(b), name, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	return takeBlob(&out), nil
}

func unprotect(b []byte) ([]byte, error) {
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(blob(b), nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	if out.Data == nil {
		return nil, errors.New("пусто")
	}
	return takeBlob(&out), nil
}
