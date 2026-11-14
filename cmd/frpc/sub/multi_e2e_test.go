package sub

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

// TestMultiServerEndToEnd exercises real frps/frpc processes and remote configuration.
func TestMultiServerEndToEnd(t *testing.T) {
	frpsBinary, frpcBinary := os.Getenv("FRP_E2E_FRPS"), os.Getenv("FRP_E2E_FRPC")
	if frpsBinary == "" || frpcBinary == "" {
		t.Skip("FRP_E2E_FRPS and FRP_E2E_FRPC are required")
	}
	temp := t.TempDir()
	freePort := func(host string) int {
		listener, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
		if err != nil {
			t.Fatal(err)
		}
		port := listener.Addr().(*net.TCPAddr).Port
		_ = listener.Close()
		return port
	}
	serverPortA, serverPortB := freePort("127.0.0.1"), freePort("127.0.0.2")
	proxyPort := freePort("127.0.0.1")
	for {
		listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.2", strconv.Itoa(proxyPort)))
		if err == nil {
			_ = listener.Close()
			break
		}
		proxyPort = freePort("127.0.0.1")
	}
	backend, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	go func() {
		for {
			conn, err := backend.Accept()
			if err != nil {
				return
			}
			go func() { defer conn.Close(); _, _ = io.Copy(conn, conn) }()
		}
	}()
	backendPort := backend.Addr().(*net.TCPAddr).Port

	writeConfig := func(name string, config any) string {
		data, err := json.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(temp, name)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	start := func(binary, config string) {
		cmd := exec.Command(binary, "-c", config)
		cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	}
	for _, server := range []struct {
		name string
		host string
		port int
	}{{"a", "127.0.0.1", serverPortA}, {"b", "127.0.0.2", serverPortB}} {
		path := writeConfig("frps-"+server.name+".json", map[string]any{
			"bindAddr": server.host, "bindPort": server.port,
			"auth": map[string]any{"method": "token", "token": "cluster-token"},
		})
		start(frpsBinary, path)
	}

	var mu sync.RWMutex
	var payload []byte
	status := http.StatusOK
	configServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") != "node-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		mu.RLock()
		defer mu.RUnlock()
		w.WriteHeader(status)
		_, _ = w.Write(payload)
	}))
	defer configServer.Close()
	serverA := map[string]any{"addr": "127.0.0.1", "port": serverPortA}
	serverB := map[string]any{"addr": "127.0.0.2", "port": serverPortB}
	setServers := func(servers ...map[string]any) []byte {
		data, err := json.Marshal(map[string]any{
			"serverAddr": "127.0.0.3", "serverPort": serverPortA, "serverAddrs": servers,
			"auth":         map[string]any{"method": "token", "token": "cluster-token"},
			"remoteConfig": map[string]any{"url": configServer.URL, "token": "node-token", "interval": 5},
			"proxies":      []map[string]any{{"name": "echo", "type": "tcp", "localIP": "127.0.0.1", "localPort": backendPort, "remotePort": proxyPort}},
		})
		if err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		payload = data
		mu.Unlock()
		return data
	}
	initial := setServers(serverA)
	clientConfig := filepath.Join(temp, "frpc.json")
	if err := os.WriteFile(clientConfig, initial, 0600); err != nil {
		t.Fatal(err)
	}
	start(frpcBinary, clientConfig)

	probe := func(host string) bool {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(proxyPort)), 300*time.Millisecond)
		if err != nil {
			return false
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(time.Second))
		if _, err := conn.Write([]byte("echo")); err != nil {
			return false
		}
		data := make([]byte, 4)
		_, err = io.ReadFull(conn, data)
		return err == nil && string(data) == "echo"
	}
	wait := func(name string, condition func() bool) {
		t.Helper()
		deadline := time.Now().Add(25 * time.Second)
		for time.Now().Before(deadline) {
			if condition() {
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s", name)
	}
	wait("single explicit server", func() bool { return probe("127.0.0.1") })
	if probe("127.0.0.2") {
		t.Fatal("proxy unexpectedly active on the second server")
	}
	setServers(serverA, serverB)
	wait("both servers", func() bool { return probe("127.0.0.1") && probe("127.0.0.2") })
	setServers(serverB)
	wait("removed server to close", func() bool { return !probe("127.0.0.1") && probe("127.0.0.2") })
	mu.Lock()
	status = http.StatusUnauthorized
	mu.Unlock()
	wait("revoked tunnel to close", func() bool { return !probe("127.0.0.2") })
	mu.Lock()
	status = http.StatusOK
	mu.Unlock()
	wait("restored tunnel to reconnect", func() bool { return probe("127.0.0.2") })
}
