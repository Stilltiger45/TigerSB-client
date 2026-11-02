package desktop

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Programs lists the running programs the user may leave out of the VPN:
// one entry per program name, without Windows' own and TigerSB's.
func Programs() ([]Program, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)
	sysDir := strings.ToLower(os.Getenv("SystemRoot"))
	if sysDir == "" {
		sysDir = `c:\windows`
	}
	own, _ := os.Executable()
	ownDir := filepath.Dir(own)
	seen := map[string]bool{}
	var out []Program
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		name := windows.UTF16ToString(e.ExeFile[:])
		key := strings.ToLower(name)
		if !strings.HasSuffix(key, ".exe") || seen[key] {
			continue
		}
		path := imagePath(e.ProcessID)
		// TigerSB's own programs: the app and the service.
		ours := sameDir(filepath.Dir(path), ownDir) && (key == "tigersb.exe" || key == "tigersb-service.exe")
		if strings.HasPrefix(strings.ToLower(path), sysDir+`\`) || ours {
			continue
		}
		seen[key] = true
		out = append(out, Program{Name: name, Path: path})
	}
	slices.SortFunc(out, func(a, b Program) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return out, nil
}

// imagePath is the program file of a process, "" when Windows does not tell.
func imagePath(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}
