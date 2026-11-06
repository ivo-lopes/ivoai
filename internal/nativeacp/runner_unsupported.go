//go:build !linux

package nativeacp

import (
	"context"
	"github.com/ivo-lopes/ivoai/internal/core"
)

func (r *Runner) Run(context.Context, core.ExecutorRequest, func(string) error) (core.ExecutorResult, error) {
	return core.ExecutorResult{}, failed("ANTIGRAVITY_PLATFORM_UNCERTIFIED")
}
