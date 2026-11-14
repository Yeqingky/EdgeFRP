package sub

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	v1 "github.com/fatedier/frp/pkg/config/v1"
)

func TestPullRemoteConfig(t *testing.T) {
	body := []byte(`{"serverAddr":"127.0.0.1","serverPort":7000}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Query().Get("token") != "secret" {
			t.Errorf("unexpected token: %q", req.URL.Query().Get("token"))
		}
		_, _ = w.Write(body)
	}))
	defer server.Close()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "frpc.json")
	if err := os.WriteFile(configPath, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &v1.ClientCommonConfig{}
	cfg.RemoteConfig.URL = server.URL
	cfg.RemoteConfig.Token = "secret"

	reloads := 0
	pullRemoteConfigWithReload(cfg, configPath, func() error {
		reloads++
		return nil
	})

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(body) {
		t.Fatalf("unexpected config: %s", data)
	}
	if reloads != 1 {
		t.Fatalf("expected one reload, got %d", reloads)
	}
}
