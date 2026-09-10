package orchestration

import (
	"bufio"
	"bytes"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// ReadHostResources has no subprocesses or network calls. Missing metrics are
// left unknown and Concurrency degrades conservatively. Linux's available RAM
// is an admission estimate, never a claim of reserved resources.
func ReadHostResources() HostResources {
	host := HostResources{CPUs: runtime.NumCPU()}
	if runtime.GOOS != "linux" {
		return host
	}
	read := func(path string) []byte {
		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer file.Close()
		buf := make([]byte, 64<<10)
		n, _ := file.Read(buf)
		return buf[:n]
	}
	return parseHostResources(host.CPUs, read("/proc/meminfo"), read("/proc/loadavg"), read("/proc/pressure/io"))
}

func parseHostResources(cpus int, memory, load, pressure []byte) HostResources {
	host := HostResources{CPUs: cpus}
	scanner := bufio.NewScanner(bytes.NewReader(memory))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 3 && fields[0] == "MemAvailable:" && fields[2] == "kB" {
			if kb, err := strconv.ParseUint(fields[1], 10, 53); err == nil {
				host.AvailableMemoryBytes = kb * 1024
			}
		}
	}
	if fields := strings.Fields(string(load)); len(fields) > 0 {
		host.Load, _ = strconv.ParseFloat(fields[0], 64)
	}
	scanner = bufio.NewScanner(bytes.NewReader(pressure))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) > 1 && fields[0] == "some" && strings.HasPrefix(fields[1], "avg10=") {
			percentage, _ := strconv.ParseFloat(strings.TrimPrefix(fields[1], "avg10="), 64)
			host.IOPressure = percentage / 100
		}
	}
	return host
}
