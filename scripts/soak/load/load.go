// Package load is the client of the soak test's bulk server: it downloads,
// uploads and keeps a stream going both ways through the VPN, as apps do.
package load

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"time"
)

// Dial opens a connection to the bulk server.
type Dial func(ctx context.Context) (net.Conn, error)

func request(c net.Conn, op byte, n int64) error {
	var req [9]byte
	req[0] = op
	binary.BigEndian.PutUint64(req[1:], uint64(n))
	_, err := c.Write(req[:])
	return err
}

// Download fetches n bytes and returns how long it took.
func Download(ctx context.Context, dial Dial, n int64) (time.Duration, error) {
	start := time.Now()
	c, err := dial(ctx)
	if err != nil {
		return 0, err
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	if err := request(c, 'D', n); err != nil {
		return 0, err
	}
	got, err := io.Copy(io.Discard, c)
	if got != n {
		return 0, fmt.Errorf("получено %d из %d байт: %v", got, n, err)
	}
	return time.Since(start), nil
}

// Upload sends n bytes and returns how long it took until the server
// confirmed them.
func Upload(ctx context.Context, dial Dial, n int64) (time.Duration, error) {
	start := time.Now()
	c, err := dial(ctx)
	if err != nil {
		return 0, err
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	if err := request(c, 'U', n); err != nil {
		return 0, err
	}
	chunk := make([]byte, 32<<10)
	for left := n; left > 0; {
		k, err := c.Write(chunk[:min(left, int64(len(chunk)))])
		if err != nil {
			return 0, err
		}
		left -= int64(k)
	}
	var ack [8]byte
	if _, err := io.ReadFull(c, ack[:]); err != nil {
		return 0, err
	}
	if got := int64(binary.BigEndian.Uint64(ack[:])); got != n {
		return 0, fmt.Errorf("сервер получил %d из %d байт", got, n)
	}
	return time.Since(start), nil
}

// StreamResult is what a stream both ways went through.
type StreamResult struct {
	Up, Down int64
	// MaxGap is the longest time no data came back; Reconnects is how many
	// times the stream had to be opened again.
	MaxGap     time.Duration
	Reconnects int
}

// Stream sends rate bytes a second to the echo of the bulk server for d and
// reads them back, opening the stream again when it breaks or stalls for
// stall, as an app would. tick, when set, gets the result so far every
// 10 seconds.
func Stream(ctx context.Context, dial Dial, d time.Duration, rate int64, stall time.Duration, tick func(StreamResult)) StreamResult {
	var r StreamResult
	var up atomic.Int64 // sent by the writer, which runs apart
	end := time.Now().Add(d)
	lastData := time.Now()
	gap := func(now time.Time) {
		if now.After(end) {
			now = end // the stream is over: silence after it does not count
		}
		if g := now.Sub(lastData); g > r.MaxGap {
			r.MaxGap = g
		}
	}
	nextTick := time.Now().Add(10 * time.Second)
	for first := true; time.Now().Before(end) && ctx.Err() == nil; first = false {
		if !first {
			r.Reconnects++
			time.Sleep(time.Second)
		}
		dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		c, err := dial(dctx)
		cancel()
		if err != nil {
			gap(time.Now())
			continue
		}
		if request(c, 'E', 0) != nil {
			c.Close()
			continue
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			chunk := make([]byte, rate/10)
			for t := time.NewTicker(100 * time.Millisecond); time.Now().Before(end); <-t.C {
				if _, err := c.Write(chunk); err != nil {
					return
				}
				up.Add(int64(len(chunk)))
			}
		}()
		buf := make([]byte, 64<<10)
		for time.Now().Before(end) {
			c.SetReadDeadline(time.Now().Add(stall))
			n, err := c.Read(buf)
			now := time.Now()
			if n > 0 {
				gap(now)
				lastData = now
				r.Down += int64(n)
			}
			if tick != nil && now.After(nextTick) {
				nextTick = now.Add(10 * time.Second)
				r.Up = up.Load()
				tick(r)
			}
			if err != nil {
				var ne net.Error
				if errors.As(err, &ne) && ne.Timeout() {
					gap(now)
				}
				break
			}
		}
		c.Close()
		<-done
	}
	gap(time.Now())
	r.Up = up.Load()
	return r
}
