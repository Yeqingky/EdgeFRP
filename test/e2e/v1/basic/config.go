package basic

import (
	"fmt"

	"github.com/onsi/ginkgo/v2"

	"github.com/fatedier/frp/test/e2e/framework"
	"github.com/fatedier/frp/test/e2e/pkg/port"
)

var _ = ginkgo.Describe("[Feature: Config]", func() {
	f := framework.NewDefaultFramework()

	ginkgo.Describe("Includes", func() {
		ginkgo.It("split tcp proxies into different files", func() {
			serverPort := f.AllocPort()
			serverConfigPath := f.GenerateConfigFile(fmt.Sprintf(`
			bindAddr = "0.0.0.0"
			bindPort = %d
			`, serverPort))

			remotePort := f.AllocPort()
			proxyConfigPath := f.GenerateConfigFile(fmt.Sprintf(`
			[[proxies]]
			name = "tcp"
			type = "tcp"
			localPort = %d
			remotePort = %d
			`, f.PortByName(framework.TCPEchoServerPort), remotePort))

			remotePort2 := f.AllocPort()
			proxyConfigPath2 := f.GenerateConfigFile(fmt.Sprintf(`
			[[proxies]]
			name = "tcp2"
			type = "tcp"
			localPort = %d
			remotePort = %d
			`, f.PortByName(framework.TCPEchoServerPort), remotePort2))

			clientConfigPath := f.GenerateConfigFile(fmt.Sprintf(`
			serverPort = %d
			includes = ["%s","%s"]
			`, serverPort, proxyConfigPath, proxyConfigPath2))

			_, _, err := f.RunFrps("-c", serverConfigPath)
			framework.ExpectNoError(err)

			_, _, err = f.RunFrpc("-c", clientConfigPath)
			framework.ExpectNoError(err)

			framework.NewRequestExpect(f).Port(remotePort).Ensure()
			framework.NewRequestExpect(f).Port(remotePort2).Ensure()
		})
	})

	ginkgo.Describe("JSON", func() {
		ginkgo.It("JSON", func() {
			serverConf := fmt.Sprintf(`{"bindPort": {{ .%s }}, "log": {"level": "trace"}}`, port.GenName("Server"))

			remotePort := f.AllocPort()
			clientConf := fmt.Sprintf(`{"serverPort": {{ .%s }}, "log": {"level": "trace"},
"proxies": [{"name": "tcp", "type": "tcp", "localPort": {{ .%s }}, "remotePort": %d}]}`,
				port.GenName("Server"), framework.TCPEchoServerPort, remotePort)

			f.RunProcesses(serverConf, []string{clientConf})
			framework.NewRequestExpect(f).Port(remotePort).Ensure()
		})
	})
})
