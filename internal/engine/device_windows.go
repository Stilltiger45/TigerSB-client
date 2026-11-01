package engine

import (
	"time"

	"golang.org/x/sys/windows"
)

// closeDevice closes the Wintun device of a stopped TUN stack, which also
// removes the adapter the core created. The stack's reading loop may wait
// on the session's read event: it is woken first and given a moment to see
// that the stack is closed, as wireguard-go does before ending a session.
func closeDevice(d any) {
	c, ok := d.(interface{ Close() error })
	if !ok {
		return
	}
	if f, ok := field(d, "readWait"); ok {
		if h, ok := f.Interface().(windows.Handle); ok && h != 0 {
			windows.SetEvent(h)
			time.Sleep(100 * time.Millisecond)
		}
	}
	c.Close()
}
