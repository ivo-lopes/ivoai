//go:build !linux || (!amd64 && !arm64)

package codesandbox

import "fmt"

// Exec fails closed on unsupported platforms.
func Exec(node string, args []string, readPaths []string, writePaths []string) error {
	return fmt.Errorf("code sandbox requires Linux amd64 or arm64 with Landlock and seccomp")
}
