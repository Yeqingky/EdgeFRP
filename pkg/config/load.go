// Copyright 2023 The frp Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/samber/lo"

	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/pkg/config/v1/validation"
	"github.com/fatedier/frp/pkg/msg"
	"github.com/fatedier/frp/pkg/util/jsonx"
	"github.com/fatedier/frp/pkg/util/util"
)

func LoadConfigureFromFile(path string, c any, strict bool) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return LoadConfigure(content, c, strict)
}

func decodeJSONContent(content []byte, target any, strict bool) error {
	if clientCfg, ok := target.(*v1.ClientConfig); ok {
		decoded, err := v1.DecodeClientConfigJSON(content, v1.DecodeOptions{
			DisallowUnknownFields: strict,
		})
		if err != nil {
			return err
		}
		*clientCfg = decoded
		return nil
	}

	return jsonx.UnmarshalWithOptions(content, target, jsonx.DecodeOptions{
		RejectUnknownMembers: strict,
	})
}

// LoadConfigure loads a JSON configuration into c.
func LoadConfigure(b []byte, c any, strict bool) error {
	if err := decodeJSONContent(b, c, strict); err != nil {
		return enhanceDecodeError(err)
	}
	return nil
}

// enhanceDecodeError adds the field path to JSON decode errors.
func enhanceDecodeError(err error) error {
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) && typeErr.Field != "" {
		return fmt.Errorf("field \"%s\": cannot unmarshal %s into %s", typeErr.Field, typeErr.Value, typeErr.Type)
	}
	return err
}

func NewProxyConfigurerFromMsg(m *msg.NewProxy, serverCfg *v1.ServerConfig) (v1.ProxyConfigurer, error) {
	m.ProxyType = util.EmptyOr(m.ProxyType, string(v1.ProxyTypeTCP))

	configurer := v1.NewProxyConfigurerByType(v1.ProxyType(m.ProxyType))
	if configurer == nil {
		return nil, fmt.Errorf("unknown proxy type: %s", m.ProxyType)
	}

	configurer.UnmarshalFromMsg(m)
	configurer.Complete()

	if err := validation.ValidateProxyConfigurerForServer(configurer, serverCfg); err != nil {
		return nil, err
	}
	return configurer, nil
}

func LoadServerConfig(path string, strict bool) (*v1.ServerConfig, error) {
	svrCfg := &v1.ServerConfig{}
	if err := LoadConfigureFromFile(path, svrCfg, strict); err != nil {
		return nil, err
	}
	if err := svrCfg.Complete(); err != nil {
		return nil, err
	}
	return svrCfg, nil
}

// ClientConfigLoadResult contains the result of loading a client configuration file.
type ClientConfigLoadResult struct {
	// Common contains the common client configuration.
	Common *v1.ClientCommonConfig

	// Proxies contains proxy configurations from inline [[proxies]] and includeConfigFiles.
	// These are NOT completed (user prefix not added).
	Proxies []v1.ProxyConfigurer

	// Visitors contains visitor configurations from inline [[visitors]] and includeConfigFiles.
	// These are NOT completed.
	Visitors []v1.VisitorConfigurer
}

// LoadClientConfigResult loads and parses a client configuration file.
// It returns the raw configuration without completing proxies/visitors.
// The caller should call Complete on the configs manually.
func LoadClientConfigResult(path string, strict bool) (*ClientConfigLoadResult, error) {
	result := &ClientConfigLoadResult{
		Proxies:  make([]v1.ProxyConfigurer, 0),
		Visitors: make([]v1.VisitorConfigurer, 0),
	}

	allCfg := v1.ClientConfig{}
	if err := LoadConfigureFromFile(path, &allCfg, strict); err != nil {
		return nil, err
	}
	result.Common = &allCfg.ClientCommonConfig
	for _, c := range allCfg.Proxies {
		result.Proxies = append(result.Proxies, c.ProxyConfigurer)
	}
	for _, c := range allCfg.Visitors {
		result.Visitors = append(result.Visitors, c.VisitorConfigurer)
	}

	// Load additional config from includes.
	if len(result.Common.IncludeConfigFiles) > 0 {
		extProxyCfgs, extVisitorCfgs, err := LoadAdditionalClientConfigs(result.Common.IncludeConfigFiles, strict)
		if err != nil {
			return nil, err
		}
		result.Proxies = append(result.Proxies, extProxyCfgs...)
		result.Visitors = append(result.Visitors, extVisitorCfgs...)
	}

	// Complete the common config
	if result.Common != nil {
		if err := result.Common.Complete(); err != nil {
			return nil, err
		}
	}

	if err := validateNoDuplicateNames(result.Proxies, result.Visitors); err != nil {
		return nil, err
	}

	return result, nil
}

func LoadClientConfig(path string, strict bool) (
	*v1.ClientCommonConfig,
	[]v1.ProxyConfigurer,
	[]v1.VisitorConfigurer,
	error,
) {
	result, err := LoadClientConfigResult(path, strict)
	if err != nil {
		return nil, nil, nil, err
	}

	proxyCfgs := result.Proxies
	visitorCfgs := result.Visitors

	proxyCfgs, visitorCfgs = FilterClientConfigurers(proxyCfgs, visitorCfgs)
	proxyCfgs = CompleteProxyConfigurers(proxyCfgs)
	visitorCfgs = CompleteVisitorConfigurers(visitorCfgs)
	return result.Common, proxyCfgs, visitorCfgs, nil
}

// validateNoDuplicateNames rejects proxies or visitors that share a name. They are
// keyed by name in the config sources, so a duplicate would otherwise be silently
// overwritten and never started, with no error or log.
func validateNoDuplicateNames(proxies []v1.ProxyConfigurer, visitors []v1.VisitorConfigurer) error {
	proxyNames := make(map[string]struct{}, len(proxies))
	for _, p := range proxies {
		name := p.GetBaseConfig().Name
		if _, ok := proxyNames[name]; ok {
			return fmt.Errorf("proxy name [%s] is duplicated", name)
		}
		proxyNames[name] = struct{}{}
	}

	visitorNames := make(map[string]struct{}, len(visitors))
	for _, v := range visitors {
		name := v.GetBaseConfig().Name
		if _, ok := visitorNames[name]; ok {
			return fmt.Errorf("visitor name [%s] is duplicated", name)
		}
		visitorNames[name] = struct{}{}
	}

	return nil
}

func CompleteProxyConfigurers(proxies []v1.ProxyConfigurer) []v1.ProxyConfigurer {
	proxyCfgs := proxies
	for _, c := range proxyCfgs {
		c.Complete()
	}
	return proxyCfgs
}

func CompleteVisitorConfigurers(visitors []v1.VisitorConfigurer) []v1.VisitorConfigurer {
	visitorCfgs := visitors
	for _, c := range visitorCfgs {
		c.Complete()
	}
	return visitorCfgs
}

func FilterClientConfigurers(
	proxies []v1.ProxyConfigurer,
	visitors []v1.VisitorConfigurer,
) ([]v1.ProxyConfigurer, []v1.VisitorConfigurer) {
	proxyCfgs := proxies
	visitorCfgs := visitors

	// Filter by enabled field in each proxy
	// nil or true means enabled, false means disabled
	proxyCfgs = lo.Filter(proxyCfgs, func(c v1.ProxyConfigurer, _ int) bool {
		enabled := c.GetBaseConfig().Enabled
		return enabled == nil || *enabled
	})
	visitorCfgs = lo.Filter(visitorCfgs, func(c v1.VisitorConfigurer, _ int) bool {
		enabled := c.GetBaseConfig().Enabled
		return enabled == nil || *enabled
	})
	return proxyCfgs, visitorCfgs
}

func LoadAdditionalClientConfigs(paths []string, strict bool) ([]v1.ProxyConfigurer, []v1.VisitorConfigurer, error) {
	proxyCfgs := make([]v1.ProxyConfigurer, 0)
	visitorCfgs := make([]v1.VisitorConfigurer, 0)
	for _, path := range paths {
		absDir, err := filepath.Abs(filepath.Dir(path))
		if err != nil {
			return nil, nil, err
		}
		if _, err := os.Stat(absDir); os.IsNotExist(err) {
			return nil, nil, err
		}
		files, err := os.ReadDir(absDir)
		if err != nil {
			return nil, nil, err
		}
		for _, fi := range files {
			if fi.IsDir() {
				continue
			}
			absFile := filepath.Join(absDir, fi.Name())
			if matched, _ := filepath.Match(filepath.Join(absDir, filepath.Base(path)), absFile); matched {
				// support yaml/json/toml
				cfg := v1.ClientConfig{}
				if err := LoadConfigureFromFile(absFile, &cfg, strict); err != nil {
					return nil, nil, fmt.Errorf("load additional config from %s error: %v", absFile, err)
				}
				for _, c := range cfg.Proxies {
					proxyCfgs = append(proxyCfgs, c.ProxyConfigurer)
				}
				for _, c := range cfg.Visitors {
					visitorCfgs = append(visitorCfgs, c.VisitorConfigurer)
				}
			}
		}
	}
	return proxyCfgs, visitorCfgs, nil
}
