//go:build race

package app

import "runtime"

func adaptiveHelperExitCode(code int) int {
	if runtime.RaceErrors() != 0 {
		return 1
	}
	return code
}
