// Command upgrade-matrix is copied into an archived published source tree and
// linked against that release's updater. It hosts release assets locally so CI
// proves the published download/checksum/extract/probe/promotion path without
// contacting GitHub or mutating a real installation.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"

	"github.com/ivo-lopes/ivoai/internal/update"
)

func main() {
	if len(os.Args) != 5 {
		panic("usage: upgrade-matrix <candidate> <installed> <rollback> <target-version>")
	}
	candidate, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "ivoai", Mode: 0o755, Size: int64(len(candidate)), Typeflag: tar.TypeReg}); err != nil {
		panic(err)
	}
	if _, err := tw.Write(candidate); err != nil {
		panic(err)
	}
	if err := tw.Close(); err != nil {
		panic(err)
	}
	if err := gz.Close(); err != nil {
		panic(err)
	}
	asset := "ivoai_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	sum := sha256.Sum256(archive.Bytes())
	checksums := []byte(fmt.Sprintf("%x  %s\n", sum, asset))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch filepath.Base(request.URL.Path) {
		case "checksums.txt":
			_, _ = w.Write(checksums)
		case asset:
			_, _ = w.Write(archive.Bytes())
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()
	checker := update.Checker{Client: server.Client(), ReleaseBase: server.URL}
	if err := checker.Apply(context.Background(), update.Release{Version: os.Args[4]}, os.Args[2], os.Args[3]); err != nil {
		panic(err)
	}
}
