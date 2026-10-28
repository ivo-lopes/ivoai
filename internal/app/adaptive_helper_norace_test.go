//go:build !race

package app

func adaptiveHelperExitCode(code int) int { return code }
