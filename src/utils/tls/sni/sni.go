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

// readTLSRecords reads TLS records from conn until we have enough data for SNI extraction
// It handles cases where ClientHello is split across multiple TCP segments or TLS records
func readTLSRecords(conn net.Conn, buf []byte) (int, error) {
	totalRead := 0
	
	for totalRead < MAX_HEADER_SIZE {
		// Read more data if we don't have enough yet
		n, err := conn.Read(buf[totalRead:])
		if err != nil {
			return totalRead, err
		}
		
		// If we got 0 bytes and no error, it means no more data is available
		if n == 0 {
			break
		}
		
		totalRead += n
		
		// If we don't have at least a TLS record header, continue reading
		if totalRead < 5 {
			continue
		}
		
		// Check if this looks like a TLS handshake record
		if buf[0] != 0x16 {
			// Not a TLS handshake record, return what we have
			break
		}
		
		// Extract record length from bytes 3-4 (big endian)
		recordLen := int(buf[3])<<8 | int(buf[4])
		totalRecordSize := 5 + recordLen // header + payload
		
		// If record length seems unreasonable, return what we have
		if recordLen > MAX_HEADER_SIZE || recordLen < 0 {
			break
		}
		
		// If we have a complete record, we're likely good for SNI extraction
		if totalRead >= totalRecordSize {
			break
		}
		
		// Continue reading until we have the complete record
		// but don't exceed our buffer size
		if totalRecordSize > MAX_HEADER_SIZE {
			break
		}
	}
	
	return totalRead, nil
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

	err := conn.SetReadDeadline(time.Now().Add(readTimeout))
	if err != nil {
		return nil, "", err
	}

	// Read data, potentially across multiple reads to handle fragmented ClientHello
	totalRead, err := readTLSRecords(conn, buf)
	if err != nil {
		return nil, "", err
	}

	err = conn.SetReadDeadline(time.Time{}) // Reset read deadline
	if err != nil {
		return nil, "", err
	}

	hostname := extractHostname(buf[0:totalRead])

	data := make([]byte, totalRead)
	copy(data, buf) // Since we reuse buf between invocations, we have to make copy of data
	mreader := io.MultiReader(bytes.NewBuffer(data), conn)

	// Wrap connection so that it will Read from buffer first and remaining data
	// from initial conn
	return Conn{mreader, conn}, hostname, nil
}
