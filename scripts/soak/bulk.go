package main

import (
	"encoding/binary"
	"io"
	"log"
	"net"
	"sync/atomic"
)

// The bulk server is what the app downloads from and uploads to. The test
// servers send every connection to BulkIP to it, so the app reaches it
// through the VPN like any address on the internet. A request is one byte of
// command and a big-endian uint64:
//
//	'D' n  the server sends n bytes and closes;
//	'U' n  the client sends n bytes, the server answers with the count;
//	'E' 0  the server echoes everything back until the client closes.
const (
	BulkIP   = "198.51.100.7"
	BulkPort = 9000
)

var bulkData = func() []byte {
	b := make([]byte, 64<<10)
	for i := range b {
		b[i] = byte(i*7 + i>>8)
	}
	return b
}()

type bulkServer struct {
	down, up atomic.Int64
}

func (s *bulkServer) serve(l net.Listener) {
	for {
		c, err := l.Accept()
		if err != nil {
			log.Print(err)
			return
		}
		go s.handle(c)
	}
}

func (s *bulkServer) handle(c net.Conn) {
	defer c.Close()
	var req [9]byte
	if _, err := io.ReadFull(c, req[:]); err != nil {
		return
	}
	n := int64(binary.BigEndian.Uint64(req[1:]))
	switch req[0] {
	case 'D':
		for n > 0 {
			k, err := c.Write(bulkData[:min(n, int64(len(bulkData)))])
			s.down.Add(int64(k))
			if err != nil {
				return
			}
			n -= int64(k)
		}
	case 'U':
		k, _ := io.CopyN(io.Discard, c, n)
		s.up.Add(k)
		var ack [8]byte
		binary.BigEndian.PutUint64(ack[:], uint64(k))
		c.Write(ack[:])
	case 'E':
		buf := make([]byte, 32<<10)
		for {
			k, err := c.Read(buf)
			if k > 0 {
				s.up.Add(int64(k))
				if _, err := c.Write(buf[:k]); err != nil {
					return
				}
				s.down.Add(int64(k))
			}
			if err != nil {
				return
			}
		}
	}
}
