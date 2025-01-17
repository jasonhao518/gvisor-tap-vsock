package dns

import (
	"net"
	"sync"

	"github.com/areYouLazy/libhosty"
	"github.com/containers/gvisor-tap-vsock/pkg/utils"
	log "github.com/sirupsen/logrus"
)

type HostsFile struct {
	hostsReadLock sync.RWMutex
	hostsFilePath string
	hostsFile     *libhosty.HostsFile
}

// NewHostsFile Creates new HostsFile instance
// Pass ""(empty string) if you want to use default hosts file
func NewHostsFile(hostsPath string) (*HostsFile, error) {
	hostsFile, err := readHostsFile(hostsPath)
	if err != nil {
		return nil, err
	}

	h := &HostsFile{
		hostsFile:     hostsFile,
		hostsFilePath: hostsFile.Config.FilePath,
	}
	if err := h.startWatch(); err != nil {
		return nil, err
	}

	return h, nil
}

func (h *HostsFile) startWatch() error {
	watcher, err := utils.NewFileWatcher(h.hostsFilePath)
	if err != nil {
		log.Errorf("Hosts file adding watcher error: %s", err)
		return err
	}

	if err := watcher.Start(h.updateHostsFile); err != nil {
		log.Errorf("Hosts file adding watcher error: %s", err)
		return err
	}

	return nil
}

func (h *HostsFile) LookupByHostname(name string) (net.IP, error) {
	h.hostsReadLock.RLock()
	defer h.hostsReadLock.RUnlock()

	_, ip, err := h.hostsFile.LookupByHostname(name)
	return ip, err
}

func (h *HostsFile) updateHostsFile() {
	newHosts, err := readHostsFile(h.hostsFilePath)
	if err != nil {
		log.Errorf("Hosts file read error:%s", err)
		return
	}

	h.hostsReadLock.Lock()
	defer h.hostsReadLock.Unlock()

	h.hostsFile = newHosts
}

func readHostsFile(hostsFilePath string) (*libhosty.HostsFile, error) {
	config, err := libhosty.NewHostsFileConfig(hostsFilePath)
	if err != nil {
		return nil, err
	}
	return libhosty.InitWithConfig(config)
}
