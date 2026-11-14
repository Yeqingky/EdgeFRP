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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	v1 "github.com/fatedier/frp/pkg/config/v1"
)

const jsonServerContent = `
{
  "bindAddr": "127.0.0.1",
  "kcpBindPort": 7000,
  "quicBindPort": 7001,
  "tcpmuxHTTPConnectPort": 7005,
  "custom404Page": "/abc.html",
  "transport": {
    "tcpKeepalive": 10
  }
}
`

func TestLoadServerConfigJSON(t *testing.T) {
	require := require.New(t)
	svrCfg := v1.ServerConfig{}
	require.NoError(LoadConfigure([]byte(jsonServerContent), &svrCfg, true))
	require.EqualValues("127.0.0.1", svrCfg.BindAddr)
	require.EqualValues(7000, svrCfg.KCPBindPort)
	require.EqualValues(7001, svrCfg.QUICBindPort)
	require.EqualValues(7005, svrCfg.TCPMuxHTTPConnectPort)
	require.EqualValues("/abc.html", svrCfg.Custom404Page)
	require.EqualValues(10, svrCfg.Transport.TCPKeepAlive)
}

func TestLoadServerConfigStrictMode(t *testing.T) {
	require := require.New(t)
	brokenContent := `{"bindAdur": "127.0.0.1"}`

	svrCfg := v1.ServerConfig{}
	require.NoError(LoadConfigure([]byte(brokenContent), &svrCfg, false))
	require.Empty(svrCfg.BindAddr)

	err := LoadConfigure([]byte(brokenContent), &svrCfg, true)
	require.ErrorContains(err, "bindAdur")
}

func TestLoadClientConfigJSON(t *testing.T) {
	require := require.New(t)
	content := `
{
  "serverAddr": "127.0.0.1",
  "serverPort": 7000,
  "proxies": [
    {
      "name": "test",
      "type": "tcp",
      "localPort": 22,
      "remotePort": 6000
    }
  ],
  "visitors": [
    {
      "name": "visitor",
      "type": "stcp",
      "serverName": "test",
      "secretKey": "secret",
      "bindPort": 9001
    }
  ]
}
`
	clientCfg := v1.ClientConfig{}
	require.NoError(LoadConfigure([]byte(content), &clientCfg, true))
	require.Len(clientCfg.Proxies, 1)
	require.Equal("test", clientCfg.Proxies[0].ProxyConfigurer.GetBaseConfig().Name)
	require.Equal(string(v1.ProxyTypeTCP), clientCfg.Proxies[0].ProxyConfigurer.GetBaseConfig().Type)
	require.Len(clientCfg.Visitors, 1)
	require.Equal("visitor", clientCfg.Visitors[0].VisitorConfigurer.GetBaseConfig().Name)
}

func TestLoadClientConfigStrictMode_UnknownPluginField(t *testing.T) {
	require := require.New(t)
	content := `
{
  "serverPort": 7000,
  "proxies": [
    {
      "name": "test",
      "type": "tcp",
      "localPort": 6000,
      "plugin": {
        "type": "http2https",
        "localAddr": "127.0.0.1:8080",
        "unknownInPlugin": "value"
      }
    }
  ]
}
`
	clientCfg := v1.ClientConfig{}
	require.NoError(LoadConfigure([]byte(content), &clientCfg, false))
	require.ErrorContains(LoadConfigure([]byte(content), &clientCfg, true), "unknownInPlugin")
}

func TestLoadClientConfigResult_DuplicateNames(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		errSubstr string
	}{
		{
			name: "duplicate proxy names",
			content: `{
  "serverAddr": "127.0.0.1",
  "serverPort": 7000,
  "proxies": [
    {"name": "dup", "type": "tcp", "localPort": 22, "remotePort": 6000},
    {"name": "dup", "type": "tcp", "localPort": 3306, "remotePort": 6001}
  ]
}`,
			errSubstr: "proxy name [dup] is duplicated",
		},
		{
			name: "duplicate visitor names",
			content: `{
  "serverAddr": "127.0.0.1",
  "serverPort": 7000,
  "visitors": [
    {"name": "dup", "type": "stcp", "serverName": "a", "secretKey": "secret", "bindPort": 9001},
    {"name": "dup", "type": "stcp", "serverName": "b", "secretKey": "secret", "bindPort": 9002}
  ]
}`,
			errSubstr: "visitor name [dup] is duplicated",
		},
		{
			name: "unique names",
			content: `{
  "serverAddr": "127.0.0.1",
  "serverPort": 7000,
  "proxies": [
    {"name": "p1", "type": "tcp", "localPort": 22, "remotePort": 6000},
    {"name": "p2", "type": "tcp", "localPort": 3306, "remotePort": 6001}
  ]
}`,
		},
		{
			name: "same name across proxy and visitor",
			content: `{
  "serverAddr": "127.0.0.1",
  "serverPort": 7000,
  "proxies": [
    {"name": "same", "type": "tcp", "localPort": 22, "remotePort": 6000}
  ],
  "visitors": [
    {"name": "same", "type": "stcp", "serverName": "a", "secretKey": "secret", "bindPort": 9001}
  ]
}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require := require.New(t)
			path := filepath.Join(t.TempDir(), "frpc.json")
			require.NoError(os.WriteFile(path, []byte(tc.content), 0o600))

			_, err := LoadClientConfigResult(path, false)
			if tc.errSubstr == "" {
				require.NoError(err)
			} else {
				require.ErrorContains(err, tc.errSubstr)
			}
		})
	}
}

func TestFilterClientConfigurers_FilterByEnabled(t *testing.T) {
	require := require.New(t)

	enabled := true
	disabled := false

	proxyKeep := &v1.TCPProxyConfig{}
	proxyKeep.Name = "keep"
	proxyKeep.Type = "tcp"
	proxyKeep.LocalPort = 10080
	proxyKeep.Enabled = &enabled

	proxyDropByEnabled := &v1.TCPProxyConfig{}
	proxyDropByEnabled.Name = "drop-by-enabled"
	proxyDropByEnabled.Type = "tcp"
	proxyDropByEnabled.LocalPort = 10082
	proxyDropByEnabled.Enabled = &disabled

	proxies, visitors := FilterClientConfigurers([]v1.ProxyConfigurer{
		proxyKeep,
		proxyDropByEnabled,
	}, nil)
	require.Len(visitors, 0)
	require.Len(proxies, 1)
	require.Equal("keep", proxies[0].GetBaseConfig().Name)
}
