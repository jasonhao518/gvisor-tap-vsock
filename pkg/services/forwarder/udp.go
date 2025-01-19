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
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
)

const LIBP2P_TAP_UDP = "/gvisor/libp2p-tap-udp/1.0.0"

func UDP(ctx context.Context, s *stack.Stack, nat map[tcpip.Address]tcpip.Address, natLock *sync.Mutex, p2pHost host.Host) *udp.Forwarder {
	p2pHost.SetStreamHandler(LIBP2P_TAP_UDP, func(stream network.Stream) {
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

		p, _ := NewUDPProxy(&autoStoppingListener{underlying: NewStreamPacketConn(stream)}, func() (net.Conn, error) {
			return net.Dial("udp", fmt.Sprintf("%s:%d", addr, num))
		})
		go p.Run()
	})

	return udp.NewForwarder(s, func(r *udp.ForwarderRequest) {
		localAddress := r.ID().LocalAddress
		p2pAddress := ""
		log.Infof("hanle udp nat: LocalAddress=%s\n", localAddress)
		if linkLocal().Contains(localAddress) || localAddress == header.IPv4Broadcast {
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

			libp2pStream, err := p2pHost.NewStream(ctx, peerID, LIBP2P_TAP_UDP)
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

			var wq waiter.Queue
			ep, tcpErr := r.CreateEndpoint(&wq)
			if tcpErr != nil {
				log.Errorf("r.CreateEndpoint() = %v", tcpErr)
				return
			}

			p, _ := NewUDPProxy(&autoStoppingListener{underlying: gonet.NewUDPConn(s, &wq, ep)}, func() (net.Conn, error) {
				//return net.Dial("udp", fmt.Sprintf("%s:%d", localAddress, r.ID().LocalPort))
				return NewStreamConn(libp2pStream), nil
			})
			go p.Run()
		} else {
			var wq waiter.Queue
			ep, tcpErr := r.CreateEndpoint(&wq)
			if tcpErr != nil {
				log.Errorf("r.CreateEndpoint() = %v", tcpErr)
				return
			}

			p, _ := NewUDPProxy(&autoStoppingListener{underlying: gonet.NewUDPConn(s, &wq, ep)}, func() (net.Conn, error) {
				return net.Dial("udp", fmt.Sprintf("%s:%d", localAddress, r.ID().LocalPort))
			})
			go p.Run()
		}
	})
}
