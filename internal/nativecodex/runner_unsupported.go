//go:build !linux

package nativecodex

import (
	"context"
	"github.com/ivo-lopes/ivoai/internal/core"
)

func (r *Runner) Run(ctx context.Context, _ core.ExecutorRequest, _ func(string) error) (core.ExecutorResult, error) {
	return core.ExecutorResult{}, contextError(ctx, failed("NATIVE_CODEX_UNAVAILABLE"))
}
