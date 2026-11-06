//go:build !linux

package ownedprocess

import (
	"errors"
	"os/exec"
	"syscall"
)

type Group struct{}

func New(*exec.Cmd) (*Group, error) {
	return nil, errors.New("owned process groups are certified on Linux only")
}
func (*Group) Signal(syscall.Signal) error { return errors.New("owned process groups unavailable") }
func (*Group) Forced() bool                { return false }
func (*Group) Wait() error                 { return errors.New("owned process groups unavailable") }
