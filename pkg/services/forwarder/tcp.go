package forwarder

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sync"

	host "github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	log "github.com/sirupsen/logrus"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/waiter"
	"inet.af/tcpproxy"
)

const linkLocalSubnet = "169.254.0.0/16"
const LIBP2P_TAP_TCP = "/gvisor/libp2p-tap-tcp/1.0.0"

func TCP(ctx context.Context, s *stack.Stack, nat map[tcpip.Address]tcpip.Address, natLock *sync.Mutex, p2pHost host.Host) *tcp.Forwarder {
	p2pHost.SetStreamHandler(LIBP2P_TAP_TCP, func(stream network.Stream) {
		buf := make([]byte, 4)

		// Read 4 bytes from the stream
		_, err := stream.Read(buf)
		if err != nil {
			log.Printf("Error reading from stream: %v", err)
			return
		}
		addr := tcpip.AddrFromSlice(buf)

		buf = make([]byte, 2)

		// Read 4 bytes from the stream
		_, err = stream.Read(buf)
		if err != nil {
			log.Printf("Error reading from stream: %v", err)
			return
		}

		// Decode the integer using BigEndian
		num := binary.BigEndian.Uint16(buf)

		log.Printf("Received number: %s %d", addr, num)
		address := tcpip.FullAddress{
			Addr: addr,
			Port: num,
		}
		conn, err := gonet.DialContextTCP(ctx, s, address, ipv4.ProtocolNumber)
		if err != nil {
			log.Printf("Error reading from stream: %v", err)
			return
		}

		remote := tcpproxy.DialProxy{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return conn, nil
			},
		}

		incoming := NewStreamConn(stream)
		if err != nil {
			log.Tracef("net.Dial() = %v", err)
		}
		remote.HandleConn(incoming)

	})
	return tcp.NewForwarder(s, 0, 10, func(r *tcp.ForwarderRequest) {
		localAddress := r.ID().LocalAddress
		p2pAddress := ""
		if linkLocal().Contains(localAddress) {
			r.Complete(true)
			return
		}

		natLock.Lock()
		if peer, found := s.GetPeerByIP(localAddress); found {
			log.Infof("Found in p2pNATMap: LocalAddress=%s, Peer=%s\n", localAddress, peer)
			p2pAddress = peer
		} else if replaced, ok := nat[localAddress]; ok {
			localAddress = replaced
		}
		natLock.Unlock()

		if p2pAddress != "" {
			log.Infof("handle p2p nat: LocalAddress=%s, Peer=%s\n", localAddress, p2pAddress)
			peerID, err := peer.Decode(p2pAddress)
			if err != nil {
				log.Warnf("Failed to parse Peer ID: %v", err)
			}

			libp2pStream, err := p2pHost.NewStream(ctx, peerID, LIBP2P_TAP_TCP)
			if err != nil {
				log.Warnf("creating stream to %s error: %v", p2pAddress, err)
				return
			}
			defer libp2pStream.Close()

			buf := make([]byte, 2) // Assuming 4 bytes (int32)
			// Encode the integer into the buffer
			binary.BigEndian.PutUint16(buf, uint16(r.ID().LocalPort))

			// Write the buffer to the stream

			addr := r.ID().LocalAddress.As4() // Now addr is addressable
			_, err2 := libp2pStream.Write(addr[:])
			if err2 != nil {
				log.Errorf("r.CreateEndpoint() = %v", err2)
			}
			_, err2 = libp2pStream.Write(buf)
			if err2 != nil {
				log.Errorf("r.CreateEndpoint() = %v", err2)
			}
			outbound := NewStreamConn(libp2pStream)
			if err != nil {
				log.Tracef("net.Dial() = %v", err)
				r.Complete(true)
				return
			}

			var wq waiter.Queue
			ep, tcpErr := r.CreateEndpoint(&wq)
			r.Complete(false)
			if tcpErr != nil {
				log.Errorf("r.CreateEndpoint() = %v", tcpErr)
				return
			}

			remote := tcpproxy.DialProxy{
				DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
					return outbound, nil
				},
			}
			remote.HandleConn(gonet.NewTCPConn(&wq, ep))

		} else {
			outbound, err := net.Dial("tcp", fmt.Sprintf("%s:%d", localAddress, r.ID().LocalPort))
			if err != nil {
				log.Tracef("net.Dial() = %v", err)
				r.Complete(true)
				return
			}

			var wq waiter.Queue
			ep, tcpErr := r.CreateEndpoint(&wq)
			r.Complete(false)
			if tcpErr != nil {
				log.Errorf("r.CreateEndpoint() = %v", tcpErr)
				return
			}

			remote := tcpproxy.DialProxy{
				DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
					return outbound, nil
				},
			}
			remote.HandleConn(gonet.NewTCPConn(&wq, ep))
		}
	})
}

func linkLocal() *tcpip.Subnet {
	_, parsedSubnet, _ := net.ParseCIDR(linkLocalSubnet) // CoreOS VM tries to connect to Amazon EC2 metadata service
	subnet, _ := tcpip.NewSubnet(tcpip.AddrFromSlice(parsedSubnet.IP), tcpip.MaskFromBytes(parsedSubnet.Mask))
	return &subnet
}
