package core

import (
	"regexp"
	"strings"
)

// LocalMetadataSource identifies deterministic host metadata, never a model
// inference, effective model, provider usage, or native executor session.
const LocalMetadataSource = "ivoai_core_local_metadata"

// LocalVersionQuestion deliberately recognizes a finite set of complete
// questions. Additional instructions, paths, history and ambiguous requests
// belong to the existing managed execution path.
func LocalVersionQuestion(prompt string) bool {
	switch strings.ToLower(strings.TrimSpace(prompt)) {
	case "qual versão deste projeto?", "qual a versão deste projeto?", "qual é a versão deste projeto?",
		"qual versão está no arquivo version?", "qual é a versão no arquivo version?",
		"what is the project version?", "what version is this project?", "what is the version in the version file?":
		return true
	}
	return false
}

var localSemver = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-((0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*))?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)

const LocalVersionMaxBytes = 256

// LocalVersionLiteral accepts only an ASCII SemVer 2.0 literal and an optional
// single terminal LF/CRLF. Source bytes are data and never instructions.
func LocalVersionLiteral(raw []byte) (string, bool) {
	if len(raw) == 0 || len(raw) > LocalVersionMaxBytes {
		return "", false
	}
	literal := strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r")
	if strings.HasSuffix(string(raw), "\r") || !localSemver.MatchString(literal) {
		return "", false
	}
	return literal, true
}
