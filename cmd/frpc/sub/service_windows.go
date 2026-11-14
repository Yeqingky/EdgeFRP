//go:build windows

package sub

import (
	"fmt"

	"github.com/fatedier/frp/pkg/policy/security"
	"golang.org/x/sys/windows/svc"
)

type frpcService struct {
	cfgFile string
}

func (s *frpcService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	changes <- svc.Status{State: svc.StartPending}
	go func() {
		if err := runClient(s.cfgFile, security.NewUnsafeFeatures(nil)); err != nil {
			fmt.Println(err)
		}
	}()
	changes <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for request := range r {
		switch request.Cmd {
		case svc.Interrogate:
			changes <- request.CurrentStatus
		case svc.Stop, svc.Shutdown:
			changes <- svc.Status{State: svc.StopPending}
			return false, 0
		}
	}
	return false, 0
}

func runService(cfgFile string) error {
	return svc.Run("frpc", &frpcService{cfgFile: cfgFile})
}
