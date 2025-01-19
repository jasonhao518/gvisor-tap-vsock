package forwarder

import (
	"bytes"
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
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/waiter"
	"inet.af/tcpproxy"
)

const linkLocalSubnet = "169.254.0.0/16"
const LIBP2P_TAP = "/gvisor/libp2p-tap/1.0.0"

func TCP(ctx context.Context, s *stack.Stack, nat map[tcpip.Address]tcpip.Address, natLock *sync.Mutex, p2pHost host.Host) *tcp.Forwarder {
	p2pHost.SetStreamHandler(LIBP2P_TAP, func(s network.Stream) {
		buf := make([]byte, 6)

		// Read 4 bytes from the stream
		_, err := s.Read(buf)
		if err != nil {
			log.Printf("Error reading from stream: %v", err)
			return
		}

		// Deserialize
		addr, err := DeserializeAddress(buf)
		if err != nil {
			fmt.Println("Deserialization error:", err)
			return
		}

		fmt.Printf("Received Address: %+v\n", addr)
	})
	return tcp.NewForwarder(s, 0, 10, func(r *tcp.ForwarderRequest) {
		localAddress := r.ID().LocalAddress
		p2pAddress := ""
		log.Infof("hanle tcp nat: LocalAddress=%s\n", localAddress)
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

			libp2pStream, err := p2pHost.NewStream(ctx, peerID, LIBP2P_TAP)
			if err != nil {
				log.Warnf("creating stream to %s error: %v", p2pAddress, err)
				return
			}
			defer libp2pStream.Close()
			addr := tcpip.FullAddress{Addr: r.ID().LocalAddress, Port: r.ID().LocalPort}
			serialized, err := SerializeAddress(addr)
			if err != nil {
				fmt.Println("Serialization error:", err)
				return
			}
			// Write the buffer to the stream
			_, err2 := libp2pStream.Write(serialized)
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

// Serialize the FullAddress into a byte slice
func SerializeAddress(addr tcpip.FullAddress) ([]byte, error) {
	buf := new(bytes.Buffer)
	// Write IP
	if err := binary.Write(buf, binary.BigEndian, addr.Addr); err != nil {
		return nil, err
	}
	// Write Port
	if err := binary.Write(buf, binary.BigEndian, addr.Port); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Deserialize a byte slice into FullAddress
func DeserializeAddress(data []byte) (tcpip.FullAddress, error) {
	buf := bytes.NewReader(data)
	var addr tcpip.FullAddress
	// Read IP
	if err := binary.Read(buf, binary.BigEndian, &addr.Addr); err != nil {
		return addr, err
	}
	// Read Port
	if err := binary.Read(buf, binary.BigEndian, &addr.Port); err != nil {
		return addr, err
	}
	return addr, nil
}
