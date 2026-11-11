package config

import (
	"context"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
)

const defaultNacosPort = 8848

// nacosConfigClient is the subset of the Nacos SDK config client used here.
type nacosConfigClient interface {
	GetConfig(param vo.ConfigParam) (string, error)
	ListenConfig(param vo.ConfigParam) error
	CancelListenConfig(param vo.ConfigParam) error
	CloseClient()
}

// NacosSource reads configuration from Nacos and listens for pushed changes.
type NacosSource struct {
	opts   NacosOptions
	client nacosConfigClient

	mu        sync.Mutex
	main      string
	transform string
	onChange  func(*Snapshot)
	listening []string
	closed    bool
}

// NewNacosSource creates a Nacos-backed configuration source.
func NewNacosSource(opts NacosOptions) (*NacosSource, error) {
	servers, err := parseNacosServers(opts.ServerAddr, opts.ContextPath)
	if err != nil {
		return nil, err
	}

	cc := constant.ClientConfig{
		NamespaceId:         opts.Namespace,
		TimeoutMs:           uint64(opts.Timeout.Milliseconds()),
		AppName:             "elasticrelay",
		Username:            opts.Username,
		Password:            opts.Password,
		AccessKey:           opts.AccessKey,
		SecretKey:           opts.SecretKey,
		CacheDir:            opts.CacheDir,
		LogDir:              opts.LogDir,
		LogLevel:            opts.LogLevel,
		NotLoadCacheAtStart: true,
		DisableUseSnapShot:  opts.StartupPolicy != StartupUseCache,
	}
	for _, s := range servers {
		if s.Scheme == "https" {
			cc.TLSCfg = constant.TLSConfig{Enable: true}
			break
		}
	}

	client, err := clients.NewConfigClient(vo.NacosClientParam{ClientConfig: &cc, ServerConfigs: servers})
	if err != nil {
		return nil, fmt.Errorf("failed to create nacos config client: %w", err)
	}
	return newNacosSourceWithClient(opts, client), nil
}

func newNacosSourceWithClient(opts NacosOptions, client nacosConfigClient) *NacosSource {
	return &NacosSource{opts: opts, client: client}
}

// parseNacosServers parses "host[:port]" entries, optionally prefixed with http:// or https://.
func parseNacosServers(addrs, contextPath string) ([]constant.ServerConfig, error) {
	if contextPath == "" {
		contextPath = "/nacos"
	}
	var servers []constant.ServerConfig
	for _, raw := range strings.Split(addrs, ",") {
		addr := strings.TrimSpace(raw)
		if addr == "" {
			continue
		}
		scheme := "http"
		switch {
		case strings.HasPrefix(addr, "https://"):
			scheme, addr = "https", strings.TrimPrefix(addr, "https://")
		case strings.HasPrefix(addr, "http://"):
			addr = strings.TrimPrefix(addr, "http://")
		}
		addr = strings.TrimSuffix(addr, "/")

		host, portStr, err := net.SplitHostPort(addr)
		port := uint64(defaultNacosPort)
		if err != nil {
			host = addr
		} else {
			port, err = strconv.ParseUint(portStr, 10, 16)
			if err != nil || port == 0 {
				return nil, fmt.Errorf("invalid nacos server port in %q", raw)
			}
		}
		if host == "" {
			return nil, fmt.Errorf("invalid nacos server address %q", raw)
		}
		servers = append(servers, *constant.NewServerConfig(host, port,
			constant.WithScheme(scheme), constant.WithContextPath(contextPath)))
	}
	if len(servers) == 0 {
		return nil, fmt.Errorf("no valid nacos server address in %q", addrs)
	}
	return servers, nil
}

// Load fetches the main configuration and the optional transform configuration.
func (n *NacosSource) Load(ctx context.Context) (*Snapshot, error) {
	main, err := n.client.GetConfig(vo.ConfigParam{DataId: n.opts.DataID, Group: n.opts.Group})
	if err != nil {
		return nil, fmt.Errorf("failed to get nacos config %s: %w", n.describeDataID(n.opts.DataID), err)
	}
	if strings.TrimSpace(main) == "" {
		return nil, fmt.Errorf("nacos config %s does not exist or is empty", n.describeDataID(n.opts.DataID))
	}

	var transform string
	if n.opts.TransformDataID != "" {
		transform, err = n.client.GetConfig(vo.ConfigParam{DataId: n.opts.TransformDataID, Group: n.opts.Group})
		if err != nil {
			return nil, fmt.Errorf("failed to get nacos config %s: %w", n.describeDataID(n.opts.TransformDataID), err)
		}
		if strings.TrimSpace(transform) == "" {
			log.Printf("⚠️  Warning: nacos transform config %s does not exist or is empty, transform runs in pass-through mode",
				n.describeDataID(n.opts.TransformDataID))
		}
	}

	n.mu.Lock()
	n.main, n.transform = main, transform
	n.mu.Unlock()

	return n.snapshot(main, transform), nil
}

// Watch registers listeners for the main and transform data IDs.
func (n *NacosSource) Watch(ctx context.Context, onChange func(*Snapshot)) error {
	n.mu.Lock()
	n.onChange = onChange
	n.mu.Unlock()

	dataIDs := []string{n.opts.DataID}
	if n.opts.TransformDataID != "" {
		dataIDs = append(dataIDs, n.opts.TransformDataID)
	}
	for _, id := range dataIDs {
		dataID := id
		err := n.client.ListenConfig(vo.ConfigParam{
			DataId: dataID,
			Group:  n.opts.Group,
			OnChange: func(namespace, group, dataId, data string) {
				n.handleChange(dataID, data)
			},
		})
		if err != nil {
			return fmt.Errorf("failed to listen nacos config %s: %w", n.describeDataID(dataID), err)
		}
		n.mu.Lock()
		n.listening = append(n.listening, dataID)
		n.mu.Unlock()
		log.Printf("ConfigSource(nacos): listening on %s", n.describeDataID(dataID))
	}

	go func() {
		<-ctx.Done()
		n.Close()
	}()
	return nil
}

func (n *NacosSource) handleChange(dataID, data string) {
	n.mu.Lock()
	if n.closed {
		n.mu.Unlock()
		return
	}
	isMain := dataID == n.opts.DataID
	if isMain && strings.TrimSpace(data) == "" {
		n.mu.Unlock()
		log.Printf("❌ ConfigSource(nacos): received empty content for %s (deleted or cleared), ignoring and keeping the current configuration",
			n.describeDataID(dataID))
		return
	}
	if isMain {
		n.main = data
	} else {
		n.transform = data
	}
	main, transform, cb := n.main, n.transform, n.onChange
	n.mu.Unlock()

	log.Printf("ConfigSource(nacos): change pushed for %s", n.describeDataID(dataID))
	if cb != nil {
		cb(n.snapshot(main, transform))
	}
}

func (n *NacosSource) snapshot(main, transform string) *Snapshot {
	var t []byte
	if transform != "" {
		t = []byte(transform)
	}
	return NewSnapshot(n.Describe(), []byte(main), t, n.opts.TransformDataID != "")
}

// Close cancels listeners and closes the client. It is safe to call multiple times.
func (n *NacosSource) Close() error {
	n.mu.Lock()
	if n.closed {
		n.mu.Unlock()
		return nil
	}
	n.closed = true
	listening := n.listening
	n.listening = nil
	n.mu.Unlock()

	for _, id := range listening {
		if err := n.client.CancelListenConfig(vo.ConfigParam{DataId: id, Group: n.opts.Group}); err != nil {
			log.Printf("ConfigSource(nacos): failed to cancel listener for %s: %v", n.describeDataID(id), err)
		}
	}
	n.client.CloseClient()
	return nil
}

// Describe returns the source description.
func (n *NacosSource) Describe() string {
	return n.describeDataID(n.opts.DataID)
}

// ReadOnly reports true: configuration is managed in Nacos.
func (n *NacosSource) ReadOnly() bool { return true }

func (n *NacosSource) describeDataID(dataID string) string {
	return fmt.Sprintf("nacos://%s/%s/%s", displayNamespace(n.opts.Namespace), n.opts.Group, dataID)
}
