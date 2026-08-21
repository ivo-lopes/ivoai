package session

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// ProcessStart returns the kernel start-time marker used to distinguish a live
// process from a recycled PID. It is intentionally empty on unsupported hosts.
func ProcessStart(pid int) string {
	if runtime.GOOS != "linux" || pid <= 0 {
		return ""
	}
	body, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return ""
	}
	// comm is parenthesized and may contain spaces. Fields after the final ')'
	// start at the documented stat field 3; starttime is field 22.
	end := strings.LastIndexByte(string(body), ')')
	if end < 0 {
		return ""
	}
	fields := strings.Fields(string(body[end+1:]))
	if len(fields) < 20 {
		return ""
	}
	if _, err := strconv.ParseUint(fields[19], 10, 64); err != nil {
		return ""
	}
	return fields[19]
}

func ProcessMatches(pid int, start string) bool {
	return pid > 0 && start != "" && ProcessStart(pid) == start
}
