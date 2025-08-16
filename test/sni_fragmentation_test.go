package test

import (
	"net"
	"testing"
	"time"

	"github.com/yyyar/gobetween/utils/tls/sni"
)

// slowReadConn simulates a connection that returns data slowly in small chunks
type slowReadConn struct {
	data     []byte
	pos      int
	maxRead  int
	readCount int
}

func (s *slowReadConn) Read(b []byte) (n int, err error) {
	if s.pos >= len(s.data) {
		return 0, nil
	}
	
	s.readCount++
	
	// Limit how much we read each time to simulate fragmentation
	maxBytes := s.maxRead
	if maxBytes == 0 {
		maxBytes = 1 // Default to 1 byte at a time
	}
	
	remaining := len(s.data) - s.pos
	toRead := maxBytes
	if toRead > remaining {
		toRead = remaining
	}
	if toRead > len(b) {
		toRead = len(b)
	}
	
	copy(b, s.data[s.pos:s.pos+toRead])
	s.pos += toRead
	return toRead, nil
}

func (s *slowReadConn) Write(b []byte) (n int, err error) { return len(b), nil }
func (s *slowReadConn) Close() error                     { return nil }
func (s *slowReadConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (s *slowReadConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (s *slowReadConn) SetDeadline(t time.Time) error    { return nil }
func (s *slowReadConn) SetReadDeadline(t time.Time) error { return nil }
func (s *slowReadConn) SetWriteDeadline(t time.Time) error { return nil }

func TestSNIWithFragmentedReads(t *testing.T) {
	// This test validates that SNI extraction handles fragmented reads
	// by ensuring multiple Read() calls are made when data comes in small chunks
	
	// Create some basic TLS-like data (this doesn't need to be a perfect ClientHello
	// since we're primarily testing the reading behavior)
	testData := []byte{
		0x16, 0x03, 0x01, 0x00, 0x10, // TLS record header: Handshake, TLS 1.0, Length 16
		0x01, 0x00, 0x00, 0x0c,       // ClientHello message header, length 12
		0x03, 0x03,                   // TLS version 1.2
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // Some data to make up the length
	}
	
	// Test with very small reads (1 byte at a time)
	slowConn := &slowReadConn{
		data:    testData,
		maxRead: 1,
	}
	
	// Call SNI sniff - it should handle the fragmented reads
	conn, hostname, err := sni.Sniff(slowConn, time.Second)
	
	// We don't expect a valid hostname since this isn't real TLS data,
	// but we should get no error and the function should handle multiple reads
	if err != nil {
		t.Fatalf("SNI sniffing failed with fragmented reads: %v", err)
	}
	
	// Verify that multiple Read calls were made (indicating it handled fragmentation)
	if slowConn.readCount < 5 {
		t.Errorf("Expected multiple Read() calls for fragmented data, got %d", slowConn.readCount)
	}
	
	// Verify the connection wrapper preserves data
	readBack := make([]byte, len(testData))
	n, err := conn.Read(readBack)
	if err != nil {
		t.Errorf("Failed to read from wrapped connection: %v", err)
	}
	
	if n != len(testData) {
		t.Errorf("Expected to read %d bytes, got %d", len(testData), n)
	}
	
	// The hostname might be empty for test data, but that's okay
	t.Logf("Fragmented read test completed successfully. Hostname: '%s', Read calls: %d", hostname, slowConn.readCount)
}

func TestSNIWithNormalReads(t *testing.T) {
	// Test that normal (non-fragmented) reads still work
	
	testData := []byte{
		0x16, 0x03, 0x01, 0x00, 0x10, // TLS record header: Handshake, TLS 1.0, Length 16
		0x01, 0x00, 0x00, 0x0c,       // ClientHello message header, length 12
		0x03, 0x03,                   // TLS version 1.2
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // Some data to make up the length
	}
	
	normalConn := &slowReadConn{
		data:    testData,
		maxRead: len(testData), // Read all at once
	}
	
	conn, hostname, err := sni.Sniff(normalConn, time.Second)
	
	if err != nil {
		t.Fatalf("SNI sniffing failed with normal reads: %v", err)
	}
	
	// Should only need one read for all the data
	if normalConn.readCount > 2 {
		t.Errorf("Expected minimal Read() calls for normal data, got %d", normalConn.readCount)
	}
	
	// Verify the connection wrapper preserves data
	readBack := make([]byte, len(testData))
	n, err := conn.Read(readBack)
	if err != nil {
		t.Errorf("Failed to read from wrapped connection: %v", err)
	}
	
	if n != len(testData) {
		t.Errorf("Expected to read %d bytes, got %d", len(testData), n)
	}
	
	t.Logf("Normal read test completed successfully. Hostname: '%s', Read calls: %d", hostname, normalConn.readCount)
}