package session

import "encoding/hex"

// MaxNativeWorkers is a metadata/process safety ceiling, not the scheduling
// decision. The live DAG/resource policy normally admits fewer workers.
const MaxNativeWorkers = 12

func validGitObjectID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
