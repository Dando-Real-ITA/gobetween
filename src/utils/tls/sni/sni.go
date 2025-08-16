package sni

/**
 * sni.go - sni sniffer implementation
 * @author Illarion Kovalchuk <illarion.kovalchuk@gmail.com>
 *
 * Package sni provides transparent access to hostname provided by ClientHello
 * message during TLS handshake.
 */

import (
	"bytes"
	"io"
	"net"
	"sync"
	"time"
)

const MAX_HEADER_SIZE = 16385

var pool = sync.Pool{
	New: func() interface{} {
		return make([]byte, MAX_HEADER_SIZE)
	},
}

// Conn delegates all calls to net.Conn, but Read to reader
type Conn struct {
	reader   io.Reader
	net.Conn //delegate
}

func (c Conn) Read(b []byte) (n int, err error) {
	return c.reader.Read(b)
}

// Sniff sniffs hostname from ClientHello message (if any),
// returns sni.Conn, filling it's Hostname field
func Sniff(conn net.Conn, readTimeout time.Duration) (net.Conn, string, error) {
	buf := pool.Get().([]byte)
	defer pool.Put(buf)

	if err := conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
		return nil, "", err
	}
	defer conn.SetReadDeadline(time.Time{}) // reset at the end

	total := 0
	var hostname string

	// Try catch TLS ClientHello
	for total < len(buf) {
		n, err := conn.Read(buf[total:])
		total += n

		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, "", err
		}

		if total >= 5 { // 5 is TLS record header size

			if buf[0] != 0x16 { // check if it's a TLS handshake
				break
			}

			if h := extractHostname(buf[:total]); h != "" {
				hostname = h
				break
			}
		}

	}

	// replay preread bytes
	data := make([]byte, total)
	copy(data, buf[:total]) // copy only what we read
	mreader := io.MultiReader(bytes.NewBuffer(data), conn)

	return Conn{reader: mreader, Conn: conn}, hostname, nil
}
