//go:build !linux || (!amd64 && !arm64)

package doctor

func codeSandboxPrerequisites() sandboxPrerequisites { return sandboxPrerequisites{} }
