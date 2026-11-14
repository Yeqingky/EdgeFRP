package sub

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fatedier/frp/client"
	"github.com/fatedier/frp/pkg/config"
	"github.com/fatedier/frp/pkg/config/source"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/pkg/config/v1/validation"
	"github.com/fatedier/frp/pkg/policy/security"
	"github.com/fatedier/frp/pkg/util/log"
)

type multiUpdate struct {
	result   *config.ClientConfigLoadResult
	proxies  []v1.ProxyConfigurer
	visitors []v1.VisitorConfigurer
	revoked  bool
}

func startMultiClient(
	result *config.ClientConfigLoadResult,
	proxyCfgs []v1.ProxyConfigurer,
	visitorCfgs []v1.VisitorConfigurer,
	unsafeFeatures *security.UnsafeFeatures,
	cfgFile string,
) error {
	closeLog := initClientLogging(result.Common)
	defer closeLog()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	paused := false

	for {
		servers, err := multiServers(result.Common, visitorCfgs)
		if err != nil {
			return err
		}
		if paused {
			servers = nil
		}

		epochCtx, cancel := context.WithCancel(ctx)
		services := make([]*client.Service, 0, len(servers))
		for index, server := range servers {
			serverVisitors := visitorCfgs
			if len(servers) > 1 && len(visitorCfgs) > 0 {
				serverVisitors = make([]v1.VisitorConfigurer, 0, len(visitorCfgs))
				for _, visitor := range visitorCfgs {
					clone := visitor.Clone().(*v1.STCPVisitorConfig)
					clone.BindUnix += fmt.Sprintf(".%d", index)
					serverVisitors = append(serverVisitors, clone)
				}
			}
			configSource := source.NewConfigSource()
			if err := configSource.ReplaceAll(proxyCfgs, serverVisitors); err != nil {
				cancel()
				return err
			}
			serverCfg := *result.Common
			serverCfg.ServerAddr, serverCfg.ServerPort = server.Addr, server.Port
			serverCfg.ServerAddrs = nil
			serverCfg.LoginFailExit = new(bool)
			if index > 0 {
				serverCfg.WebServer.Port = 0
				serverCfg.MetricsServer.Port = 0
			}
			svr, err := client.NewService(client.ServiceOptions{
				Common: &serverCfg, ConfigSourceAggregator: source.NewAggregator(configSource),
				UnsafeFeatures: unsafeFeatures, ConfigFilePath: cfgFile,
			})
			if err != nil {
				for _, existing := range services {
					existing.Close()
				}
				cancel()
				return err
			}
			services = append(services, svr)
		}

		log.Infof("start frpc service for config file [%s] with %d servers", cfgFile, len(services))
		finished := make(chan error, len(services))
		for _, service := range services {
			go func(s *client.Service) { finished <- s.Run(epochCtx) }(service)
		}
		updates := make(chan multiUpdate, 1)
		startMultiRemotePoller(epochCtx, result.Common, cfgFile, unsafeFeatures, paused, updates)

		var next *multiUpdate
		var runErr error
		remaining := len(services)
		select {
		case <-ctx.Done():
		case runErr = <-finished:
			remaining--
		case update := <-updates:
			next = &update
		}
		cancel()
		for _, service := range services {
			service.GracefulClose(500 * time.Millisecond)
		}
		deadline := time.NewTimer(15 * time.Second)
		for remaining > 0 {
			select {
			case <-finished:
				remaining--
			case <-deadline.C:
				return errors.New("timed out stopping frpc services")
			}
		}
		deadline.Stop()
		if ctx.Err() != nil {
			return nil
		}
		if next == nil {
			return runErr
		}
		if next.revoked {
			paused = true
			log.Warnf("tunnel authorization revoked; connections stopped")
		} else {
			paused = false
			result, proxyCfgs, visitorCfgs = next.result, next.proxies, next.visitors
		}
	}
}

func startMultiRemotePoller(ctx context.Context, cfg *v1.ClientCommonConfig, cfgFile string, unsafeFeatures *security.UnsafeFeatures, paused bool, updates chan<- multiUpdate) {
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
				pullRemoteConfigWithRevocation(cfg, cfgFile, func() error {
					result, err := config.LoadClientConfigResult(cfgFile, true)
					if err != nil {
						return err
					}
					proxies, visitors := config.FilterClientConfigurers(result.Proxies, result.Visitors)
					proxies = config.CompleteProxyConfigurers(proxies)
					visitors = config.CompleteVisitorConfigurers(visitors)
					if _, err := validation.ValidateAllClientConfig(result.Common, proxies, visitors, unsafeFeatures); err != nil {
						return err
					}
					if _, err := multiServers(result.Common, visitors); err != nil {
						return err
					}
					select {
					case updates <- multiUpdate{result: result, proxies: proxies, visitors: visitors}:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				}, paused, func() {
					if !paused {
						select {
						case updates <- multiUpdate{revoked: true}:
						case <-ctx.Done():
						}
					}
				})
				timer.Reset(interval)
			}
		}
	}()
}

func multiServers(cfg *v1.ClientCommonConfig, visitors []v1.VisitorConfigurer) ([]v1.ServerAddrConfig, error) {
	if cfg.Store.IsEnabled() {
		return nil, errors.New("store is not supported with multiple servers")
	}
	servers := cfg.ServerAddrs
	if len(servers) == 0 {
		servers = []v1.ServerAddrConfig{{Addr: cfg.ServerAddr, Port: cfg.ServerPort}}
	}
	if len(servers) > 1 {
		for _, visitor := range visitors {
			stcp, ok := visitor.(*v1.STCPVisitorConfig)
			if !ok || stcp.BindUnix == "" {
				return nil, errors.New("multiple servers require STCP visitors with bindUnix")
			}
		}
	}
	seen := make(map[v1.ServerAddrConfig]bool, len(servers))
	for _, server := range servers {
		if server.Addr == "" || server.Addr == "0.0.0.0" || server.Addr == "::" || server.Port < 1 || server.Port > 65535 || seen[server] {
			return nil, fmt.Errorf("invalid or duplicate server address %q:%d", server.Addr, server.Port)
		}
		seen[server] = true
	}
	return servers, nil
}
