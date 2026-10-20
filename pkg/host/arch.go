package host

import (
	"bytes"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Returns the host machine architecture, not what the program was compiled with
func GetMachineArch() string {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "windows":
		// Windows stores the architecture in environment variables
		arch := os.Getenv("PROCESSOR_ARCHITEW6432")
		if arch == "" {
			arch = os.Getenv("PROCESSOR_ARCHITECTURE")
		}
		return arch
	default:
		// Unix-like systems (Linux, macOS, FreeBSD) use `uname -m`
		cmd = exec.Command("uname", "-m")
	}

	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return "unknown"
	}

	return strings.TrimSpace(out.String())
}
