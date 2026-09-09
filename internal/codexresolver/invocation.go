package codexresolver

import "strings"

// ExecInvocation separates root configuration from exec-only options. In
// particular, config overrides must remain together at the root parsing level;
// repository checking is an exec concern, not a sandbox/trust override.
type ExecInvocation struct {
	GlobalArgs []string
	ExecArgs   []string
}

var execOptions = map[string]bool{
	"--skip-git-repo-check": false, "--ephemeral": false,
	"--ignore-user-config": false, "--ignore-rules": false,
	"--output-schema": true, "--output-last-message": true, "-o": true,
	"--thread-source": true, "--color": true, "--json": false,
}

// SplitExecArguments adapts legacy mixed CLI options at the managed boundary.
// Values are consumed with their option so a value spelling an exec flag is
// never mistaken for an option. Unknown options retain the previous root scope.
func SplitExecArguments(input []string) ExecInvocation {
	var result ExecInvocation
	valueOptions := map[string]bool{
		"-c": true, "--config": true, "--enable": true, "--disable": true,
		"--model": true, "-m": true, "--profile": true, "-p": true,
		"--sandbox": true, "-s": true, "--ask-for-approval": true, "-a": true,
		"--cd": true, "-C": true, "--add-dir": true, "--image": true, "-i": true,
		"--local-provider": true,
	}
	for i := 0; i < len(input); i++ {
		option, _, inline := strings.Cut(input[i], "=")
		target := &result.GlobalArgs
		needsValue := valueOptions[option]
		if takesValue, ok := execOptions[option]; ok {
			target, needsValue = &result.ExecArgs, takesValue
		}
		*target = append(*target, input[i])
		if needsValue && !inline && i+1 < len(input) {
			i++
			*target = append(*target, input[i])
		}
	}
	return result
}

// Arguments builds a controlled invocation. Repository checks are explicitly
// skipped because administrative non-Git workspaces are supported by IVOAI;
// executor sandbox and approval settings are not changed here.
func (v ExecInvocation) Arguments(resumeID, directory string, selection []string) []string {
	args := append([]string(nil), v.GlobalArgs...)
	args = append(args, "exec")
	// Managed framing has one owner. Clap rejects repeated options even when
	// values are equal; consume option values before removing our fixed flags.
	for i := 0; i < len(v.ExecArgs); i++ {
		option, _, inline := strings.Cut(v.ExecArgs[i], "=")
		end := i
		if execOptions[option] && !inline && i+1 < len(v.ExecArgs) {
			end++
		}
		if option != "--skip-git-repo-check" && option != "--color" && option != "--json" {
			args = append(args, v.ExecArgs[i:end+1]...)
		}
		i = end
	}
	args = append(args, "--skip-git-repo-check")
	args = append(args, "--color", "never", "-C", directory)
	if resumeID != "" {
		args = append(args, "resume")
	}
	args = append(args, "--json")
	args = append(args, selection...)
	if resumeID != "" {
		args = append(args, resumeID)
	}
	return ConfigurationArgs(append(args, "-"))
}
