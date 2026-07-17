package proxyprotocol

import (
	"net"

	proxyproto "github.com/pires/go-proxyproto"
)

var ErrInvalidProxyProtocolAddress = proxyproto.ErrInvalidAddress

// / SendProxyProtocolV1 sends a proxy protocol v1 header to initialize the connection
// / https://www.haproxy.org/download/1.8/doc/proxy-protocol.txt
func SendProxyProtocolV1(client net.Conn, backend net.Conn) error {
	return SendProxyProtocolV1Addrs(client.RemoteAddr(), client.LocalAddr(), backend)
}

// SendProxyProtocolV1Addrs sends a proxy protocol v1 header using explicit source and destination addresses.
func SendProxyProtocolV1Addrs(source net.Addr, destination net.Addr, backend net.Conn) error {
	return SendProxyProtocolAddrs("1", source, destination, backend)
}

// SendProxyProtocolV2Addrs sends a proxy protocol v2 header using explicit source and destination addresses.
func SendProxyProtocolV2Addrs(source net.Addr, destination net.Addr, backend net.Conn) error {
	return SendProxyProtocolAddrs("2", source, destination, backend)
}

// SendProxyProtocolAddrs sends a proxy protocol header using explicit source and destination addresses.
func SendProxyProtocolAddrs(version string, source net.Addr, destination net.Addr, backend net.Conn) error {
	var versionByte byte
	switch version {
	case "1":
		versionByte = 1
	case "2":
		versionByte = 2
	default:
		return proxyproto.ErrUnknownProxyProtocolVersion
	}

	header := proxyproto.HeaderProxyFromAddrs(versionByte, source, destination)
	if header.Command == proxyproto.LOCAL {
		return ErrInvalidProxyProtocolAddress
	}

	if !header.TransportProtocol.IsStream() {
		return ErrInvalidProxyProtocolAddress
	}

	_, err := header.WriteTo(backend)
	return err
}

// PrependProxyProtocolV2Datagram prepends a v2 datagram PROXY header to a UDP payload.
func PrependProxyProtocolV2Datagram(source net.Addr, destination net.Addr, payload []byte) ([]byte, error) {
	header := proxyproto.HeaderProxyFromAddrs(2, source, destination)
	if header.Command == proxyproto.LOCAL {
		return nil, ErrInvalidProxyProtocolAddress
	}

	if !header.TransportProtocol.IsDatagram() {
		return nil, ErrInvalidProxyProtocolAddress
	}

	headerBytes, err := header.Format()
	if err != nil {
		return nil, err
	}

	packet := make([]byte, 0, len(headerBytes)+len(payload))
	packet = append(packet, headerBytes...)
	packet = append(packet, payload...)
	return packet, nil
}
