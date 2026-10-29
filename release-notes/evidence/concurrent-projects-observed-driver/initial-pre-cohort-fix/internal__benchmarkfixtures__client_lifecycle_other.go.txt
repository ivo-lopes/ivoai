//go:build !linux

package benchmarkfixtures

import (
	"errors"
	"os/exec"
)

func RunObservedClient(_ *exec.Cmd, _ ClientIdentity, _, _ string) (*ClientLifecycle, error) {
	return nil, errors.New("client lifecycle observation requires Linux")
}
