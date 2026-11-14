//go:build !windows

package sub

import "errors"

func runService(cfgFile string) error {
	return errors.New("--service is only supported on Windows")
}
