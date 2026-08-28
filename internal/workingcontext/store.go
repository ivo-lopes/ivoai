package workingcontext

import (
	"context"
	"io"
	"time"
)

type PutRequest struct {
	Kind        ArtifactKind
	MediaType   string
	Owner       Ownership
	Sensitivity Sensitivity
	TTL         time.Duration
	Truncated   bool
}

type ArtifactWriter interface {
	Put(context.Context, PutRequest, io.Reader) (ArtifactRef, error)
}

type ArtifactReader interface {
	Stat(context.Context, Ownership, string) (ArtifactRef, error)
	Read(context.Context, Ownership, string) (io.ReadCloser, ArtifactRef, error)
	ReadRange(context.Context, Ownership, string, int64, int64) ([]byte, ArtifactRef, error)
}

type ArtifactMaintainer interface {
	GC(context.Context) (int, error)
}

type ArtifactStore interface {
	ArtifactWriter
	ArtifactReader
	ArtifactMaintainer
}
