package forwarder

import (
	"context"
	"fmt"
	"net"
	"sync"

	host "github.com/libp2p/go-libp2p/core/host"
	log "github.com/sirupsen/logrus"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
)

func UDP(ctx context.Context, s *stack.Stack, nat map[tcpip.Address]tcpip.Address, natLock *sync.Mutex, p2pHost host.Host) *udp.Forwarder {
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
