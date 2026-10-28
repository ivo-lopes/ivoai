//go:build !linux || !amd64

package rtkview

import (
	"context"
	"errors"
)

var ErrUnavailable = errors.New("RTK OSS view unavailable")

type Provider struct{}

func New(binary, helper string) *Provider { return &Provider{} }

func (*Provider) Transform(ctx context.Context, filter string, raw []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, ErrUnavailable
}
