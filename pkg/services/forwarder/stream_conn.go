package forwarder

import (
	"net"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
)

type StreamConn struct {
	stream network.Stream
}

func NewStreamConn(stream network.Stream) net.Conn {
	return &StreamConn{stream: stream}
}

// Read reads data from the stream
func (sc *StreamConn) Read(b []byte) (int, error) {
	return sc.stream.Read(b)
}

// Write writes data to the stream
func (sc *StreamConn) Write(b []byte) (int, error) {
	return sc.stream.Write(b)
}

// Close closes the stream
func (sc *StreamConn) Close() error {
	return sc.stream.Close()
}

// LocalAddr returns a dummy local address
func (sc *StreamConn) LocalAddr() net.Addr {
	addr, _ := net.ResolveTCPAddr("tcp", "192.168.127.2:8080")
	return addr
}

// RemoteAddr returns a dummy remote address
func (sc *StreamConn) RemoteAddr() net.Addr {
	addr, _ := net.ResolveTCPAddr("tcp", "192.168.127.2:8080")
	return addr
}

// SetDeadline sets the read and write deadlines
func (sc *StreamConn) SetDeadline(t time.Time) error {
	if err := sc.SetReadDeadline(t); err != nil {
		return err
	}
	return sc.SetWriteDeadline(t)
}

// SetReadDeadline sets the read deadline
func (sc *StreamConn) SetReadDeadline(t time.Time) error {
	return sc.stream.SetReadDeadline(t)
}

// SetWriteDeadline sets the write deadline
func (sc *StreamConn) SetWriteDeadline(t time.Time) error {
	return sc.stream.SetWriteDeadline(t)
}
