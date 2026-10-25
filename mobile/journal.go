package mobile

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

// The journal is what the user sends when the VPN misbehaves: the app's own
// events (events.log), Xray's warnings (xray.log) and the trace of the last
// crash of the core (crash.log). Each file is kept small.
const (
	eventsFile = "events.log"
	xrayLog    = "xray.log"
	crashFile  = "crash.log"

	maxLog = 256 << 10
)

var journalMu sync.Mutex

// startJournal sends a fatal error of the Go runtime to crash.log, so the
// trace of a crash outlives the process.
func startJournal() {
	path := filepath.Join(dataDir, crashFile)
	trim(path)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	debug.SetCrashOutput(f, debug.CrashOptions{})
	f.Close()
}

// Note adds a line to the journal: what the VPN did and why.
func Note(event string) {
	journalMu.Lock()
	defer journalMu.Unlock()
	path := filepath.Join(dataDir, eventsFile)
	trim(path)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("01-02 15:04:05"), strings.TrimSpace(event))
}

// Journal returns the journal as text to send: the last events, the last
// warnings of Xray and the last crash, if any.
func Journal() string {
	journalMu.Lock()
	defer journalMu.Unlock()
	var b strings.Builder
	part := func(title, file string, lines int) {
		t := tail(filepath.Join(dataDir, file), lines)
		if t == "" {
			return
		}
		fmt.Fprintf(&b, "== %s ==\n%s\n", title, t)
	}
	part("События", eventsFile, 200)
	part("Сбой ядра", crashFile, 120)
	part("Xray", xrayLog, 150)
	if b.Len() == 0 {
		return "Журнал пуст"
	}
	return b.String()
}

// trim keeps the newer half of a file that grew over maxLog.
func trim(path string) {
	b, err := os.ReadFile(path)
	if err != nil || len(b) <= maxLog {
		return
	}
	b = b[len(b)-maxLog/2:]
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		b = b[i+1:]
	}
	os.WriteFile(path, b, 0o600)
}

func tail(path string, lines int) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	all := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	return strings.Join(all, "\n")
}
