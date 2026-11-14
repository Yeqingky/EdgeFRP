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
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	v1 "github.com/fatedier/frp/pkg/config/v1"
)

// WordSepNormalizeFunc changes all flags that contain "_" separators.
func WordSepNormalizeFunc(f *pflag.FlagSet, name string) pflag.NormalizedName {
	if strings.Contains(name, "_") {
		return pflag.NormalizedName(strings.ReplaceAll(name, "_", "-"))
	}
	return pflag.NormalizedName(name)
}

// RegisterSSHProxyFlags registers the proxy options accepted by the SSH tunnel
// gateway command payload.
func RegisterSSHProxyFlags(cmd *cobra.Command, c v1.ProxyConfigurer) {
	cmd.Flags().StringVarP(&c.GetBaseConfig().Name, "proxy_name", "n", "", "proxy name")
	cmd.Flags().StringToStringVarP(&c.GetBaseConfig().Metadatas, "metadatas", "", nil, "metadata key-value pairs (e.g., key1=value1,key2=value2)")
	cmd.Flags().StringToStringVarP(&c.GetBaseConfig().Annotations, "annotations", "", nil, "annotation key-value pairs (e.g., key1=value1,key2=value2)")

	switch cc := c.(type) {
	case *v1.TCPProxyConfig:
		cmd.Flags().IntVarP(&cc.RemotePort, "remote_port", "r", 0, "remote port")
	case *v1.UDPProxyConfig:
		cmd.Flags().IntVarP(&cc.RemotePort, "remote_port", "r", 0, "remote port")
	case *v1.HTTPProxyConfig:
		registerProxyDomainConfigFlags(cmd, &cc.DomainConfig)
		cmd.Flags().StringSliceVarP(&cc.Locations, "locations", "", []string{}, "locations")
		cmd.Flags().StringVarP(&cc.HTTPUser, "http_user", "", "", "http auth user")
		cmd.Flags().StringVarP(&cc.HTTPPassword, "http_pwd", "", "", "http auth password")
		cmd.Flags().StringVarP(&cc.HostHeaderRewrite, "host_header_rewrite", "", "", "host header rewrite")
	case *v1.HTTPSProxyConfig:
		registerProxyDomainConfigFlags(cmd, &cc.DomainConfig)
	case *v1.TCPMuxProxyConfig:
		registerProxyDomainConfigFlags(cmd, &cc.DomainConfig)
		cmd.Flags().StringVarP(&cc.Multiplexer, "mux", "", "", "multiplexer")
		cmd.Flags().StringVarP(&cc.HTTPUser, "http_user", "", "", "http auth user")
		cmd.Flags().StringVarP(&cc.HTTPPassword, "http_pwd", "", "", "http auth password")
	case *v1.STCPProxyConfig:
		cmd.Flags().StringVarP(&cc.Secretkey, "sk", "", "", "secret key")
		cmd.Flags().StringSliceVarP(&cc.AllowUsers, "allow_users", "", []string{}, "allow visitor users")
	case *v1.SUDPProxyConfig:
		cmd.Flags().StringVarP(&cc.Secretkey, "sk", "", "", "secret key")
		cmd.Flags().StringSliceVarP(&cc.AllowUsers, "allow_users", "", []string{}, "allow visitor users")
	case *v1.XTCPProxyConfig:
		cmd.Flags().StringVarP(&cc.Secretkey, "sk", "", "", "secret key")
		cmd.Flags().StringSliceVarP(&cc.AllowUsers, "allow_users", "", []string{}, "allow visitor users")
	}
}

func registerProxyDomainConfigFlags(cmd *cobra.Command, c *v1.DomainConfig) {
	if c == nil {
		return
	}
	cmd.Flags().StringSliceVarP(&c.CustomDomains, "custom_domain", "d", []string{}, "custom domains")
	cmd.Flags().StringVarP(&c.SubDomain, "sd", "", "", "sub domain")
}

// RegisterSSHClientCommonFlags registers the client options accepted by the SSH
// tunnel gateway command payload.
func RegisterSSHClientCommonFlags(cmd *cobra.Command, c *v1.ClientCommonConfig) {
	cmd.PersistentFlags().StringVarP(&c.User, "user", "u", "", "user")
	cmd.PersistentFlags().StringVar(&c.ClientID, "client-id", "", "unique identifier for this frpc instance")
	cmd.PersistentFlags().StringVarP(&c.Auth.Token, "token", "t", "", "auth token")
}
