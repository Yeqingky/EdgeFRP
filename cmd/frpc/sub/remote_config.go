package sub

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/fatedier/frp/client"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/pkg/util/log"
)

const maxRemoteConfigSize = 4 << 20

func startRemoteConfigPoller(ctx context.Context, cfg *v1.ClientCommonConfig, cfgFile string, svr *client.Service) {
	if cfg.RemoteConfig.URL == "" || cfgFile == "" {
		return
	}
	interval := time.Duration(cfg.RemoteConfig.Interval) * time.Second
	if interval < 5*time.Second {
		interval = 30 * time.Second
	}
	go func() {
		timer := time.NewTimer(time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				pullRemoteConfig(cfg, cfgFile, svr)
				timer.Reset(interval)
			}
		}
	}()
}

func pullRemoteConfig(cfg *v1.ClientCommonConfig, cfgFile string, svr *client.Service) {
	pullRemoteConfigWithReload(cfg, cfgFile, svr.ReloadFromFile)
}

func pullRemoteConfigWithReload(cfg *v1.ClientCommonConfig, cfgFile string, reload func() error) {
	endpoint, err := url.Parse(cfg.RemoteConfig.URL)
	if err != nil {
		log.Warnf("remote config: invalid url: %v", err)
		return
	}
	if cfg.RemoteConfig.Token != "" {
		query := endpoint.Query()
		query.Set("token", cfg.RemoteConfig.Token)
		endpoint.RawQuery = query.Encode()
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(endpoint.String())
	if err != nil {
		log.Warnf("remote config: fetch failed: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Warnf("remote config: unexpected status %d", resp.StatusCode)
		return
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxRemoteConfigSize+1))
	if err != nil {
		log.Warnf("remote config: read failed: %v", err)
		return
	}
	if len(data) > maxRemoteConfigSize {
		log.Warnf("remote config: response is too large")
		return
	}

	current, err := os.ReadFile(cfgFile)
	if err == nil && bytes.Equal(current, data) {
		return
	}

	dir := filepath.Dir(cfgFile)
	temp, err := os.CreateTemp(dir, ".frpc-*.json")
	if err != nil {
		log.Warnf("remote config: create temp file failed: %v", err)
		return
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		log.Warnf("remote config: write temp file failed: %v", err)
		return
	}
	if err := temp.Chmod(0600); err != nil {
		_ = temp.Close()
		log.Warnf("remote config: chmod temp file failed: %v", err)
		return
	}
	if err := temp.Close(); err != nil {
		log.Warnf("remote config: close temp file failed: %v", err)
		return
	}
	if err := os.Rename(tempName, cfgFile); err != nil {
		log.Warnf("remote config: replace config failed: %v", err)
		return
	}

	if err := reload(); err != nil {
		if len(current) > 0 {
			_ = os.WriteFile(cfgFile, current, 0600)
		}
		log.Warnf("remote config: reload failed: %v", err)
		return
	}
	log.Infof("remote config reloaded")
}
