package config

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/nacos-group/nacos-sdk-go/v2/vo"
)

type fakeNacosClient struct {
	mu        sync.Mutex
	configs   map[string]string
	getErr    error
	listeners map[string]func(namespace, group, dataId, data string)
	cancelled []string
	closed    bool
}

func newFakeNacosClient() *fakeNacosClient {
	return &fakeNacosClient{configs: map[string]string{}, listeners: map[string]func(string, string, string, string){}}
}

func (f *fakeNacosClient) GetConfig(p vo.ConfigParam) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return "", f.getErr
	}
	return f.configs[p.Group+"/"+p.DataId], nil
}

func (f *fakeNacosClient) ListenConfig(p vo.ConfigParam) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listeners[p.Group+"/"+p.DataId] = p.OnChange
	return nil
}

func (f *fakeNacosClient) CancelListenConfig(p vo.ConfigParam) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelled = append(f.cancelled, p.DataId)
	return nil
}

func (f *fakeNacosClient) CloseClient() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
}

func (f *fakeNacosClient) push(group, dataID, data string) {
	f.mu.Lock()
	cb := f.listeners[group+"/"+dataID]
	f.mu.Unlock()
	if cb != nil {
		cb("esrelay", group, dataID, data)
	}
}

func testNacosOptions() NacosOptions {
	return NacosOptions{Namespace: "esrelay", Group: "test", DataID: "elasticrelay.json", TransformDataID: "elasticrelay-transform.json"}
}

func TestNacosSourceLoad(t *testing.T) {
	client := newFakeNacosClient()
	client.configs["test/elasticrelay.json"] = validMultiJSON
	client.configs["test/elasticrelay-transform.json"] = `{"transform_rules":[]}`
	src := newNacosSourceWithClient(testNacosOptions(), client)

	snap, err := src.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if string(snap.Main) != validMultiJSON || string(snap.Transform) != `{"transform_rules":[]}` || !snap.HasTransform {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
	if snap.Origin != "nacos://esrelay/test/elasticrelay.json" || snap.MainRevision == "" {
		t.Fatalf("unexpected origin/revision: %s %s", snap.Origin, snap.MainRevision)
	}
}

func TestNacosSourceLoadFailures(t *testing.T) {
	client := newFakeNacosClient()
	src := newNacosSourceWithClient(testNacosOptions(), client)
	if _, err := src.Load(context.Background()); err == nil || !strings.Contains(err.Error(), "does not exist or is empty") {
		t.Fatalf("missing config must fail, got %v", err)
	}

	client.getErr = errors.New("connection refused")
	if _, err := src.Load(context.Background()); err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("client error must fail, got %v", err)
	}
}

func TestNacosSourceWatch(t *testing.T) {
	client := newFakeNacosClient()
	client.configs["test/elasticrelay.json"] = validMultiJSON
	src := newNacosSourceWithClient(testNacosOptions(), client)
	if _, err := src.Load(context.Background()); err != nil {
		t.Fatal(err)
	}

	var got []*Snapshot
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := src.Watch(ctx, func(s *Snapshot) { got = append(got, s) }); err != nil {
		t.Fatal(err)
	}

	client.push("test", "elasticrelay.json", `{"version":"3.0"}`)
	client.push("test", "elasticrelay.json", "  ")
	client.push("test", "elasticrelay-transform.json", `{"transform_rules":[{"id":"r"}]}`)

	if len(got) != 2 {
		t.Fatalf("empty main content must be ignored; got %d callbacks", len(got))
	}
	if string(got[0].Main) != `{"version":"3.0"}` {
		t.Fatalf("unexpected main after push: %s", got[0].Main)
	}
	if string(got[1].Main) != `{"version":"3.0"}` || !strings.Contains(string(got[1].Transform), `"id":"r"`) {
		t.Fatalf("transform push must carry latest main and transform: %+v", got[1])
	}

	if err := src.Close(); err != nil {
		t.Fatal(err)
	}
	if !client.closed || len(client.cancelled) != 2 {
		t.Fatalf("close must cancel listeners and close client: closed=%t cancelled=%v", client.closed, client.cancelled)
	}
	client.push("test", "elasticrelay.json", `{"version":"2.0"}`)
	if len(got) != 2 {
		t.Fatal("no callbacks expected after close")
	}
	if err := src.Close(); err != nil {
		t.Fatal("close must be idempotent")
	}
}

func TestParseNacosServers(t *testing.T) {
	servers, err := parseNacosServers("http://172.168.0.141/, https://n2.example.com, n3, 10.0.0.1:18848", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 4 {
		t.Fatalf("want 4 servers, got %d", len(servers))
	}
	if servers[0].IpAddr != "172.168.0.141" || servers[0].Port != 8848 || servers[0].Scheme != "http" || servers[0].ContextPath != "/nacos" {
		t.Errorf("unexpected server 0: %+v", servers[0])
	}
	if servers[1].IpAddr != "n2.example.com" || servers[1].Port != 8848 || servers[1].Scheme != "https" {
		t.Errorf("unexpected server 1: %+v", servers[1])
	}
	if servers[2].IpAddr != "n3" || servers[2].Port != 8848 {
		t.Errorf("unexpected server 2: %+v", servers[2])
	}
	if servers[3].IpAddr != "10.0.0.1" || servers[3].Port != 18848 {
		t.Errorf("unexpected server 3: %+v", servers[3])
	}
	for _, bad := range []string{"", " , ", "host:abc", "host:0"} {
		if _, err := parseNacosServers(bad, ""); err == nil {
			t.Errorf("parseNacosServers(%q) should fail", bad)
		}
	}
}
