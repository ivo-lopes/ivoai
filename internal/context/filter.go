package context

import (
	"bytes"
	"path/filepath"
	"strings"
)

var sensitiveNames = map[string]struct{}{
	".env": {}, ".npmrc": {}, ".pypirc": {}, ".netrc": {},
	".git-credentials": {}, ".vault-token": {},
	"credentials": {}, "credentials.json": {}, "secrets.json": {},
	"terraform.tfstate": {},
	"id_rsa":            {}, "id_ed25519": {}, "known_hosts": {},
}

var sensitiveSuffixes = []string{
	".env",
	".pem", ".key", ".p12", ".pfx", ".jks", ".keystore", ".kdbx",
	".tfstate", ".tfstate.backup", ".tfvars", ".tfvars.json",
}

// SafeDocumentPath rejects common credentials, VCS internals, dependency trees,
// device paths, symlinks (checked by connectors), and dot-env variants.
func SafeDocumentPath(path string) bool {
	clean := filepath.Clean(path)
	base := strings.ToLower(filepath.Base(clean))
	if _, found := sensitiveNames[base]; found || strings.HasPrefix(base, ".env.") {
		return false
	}
	for _, suffix := range sensitiveSuffixes {
		if strings.HasSuffix(base, suffix) {
			return false
		}
	}
	parts := strings.Split(filepath.ToSlash(clean), "/")
	for index, part := range parts {
		switch strings.ToLower(part) {
		case ".git", ".ssh", ".gnupg", ".aws", ".kube", ".docker", ".config/gcloud", "node_modules", "vendor", ".terraform":
			return false
		}
		// filepath splitting means gcloud must be recognized as the child of
		// .config rather than as the combined literal above.
		if index > 0 && strings.EqualFold(parts[index-1], ".config") && strings.EqualFold(part, "gcloud") {
			return false
		}
	}
	return true
}

// LooksTextual applies a conservative binary-file heuristic.
func LooksTextual(data []byte) bool {
	if bytes.IndexByte(data, 0) >= 0 {
		return false
	}
	var controls int
	for _, b := range data {
		if b < 0x20 && b != '\n' && b != '\r' && b != '\t' && b != '\f' {
			controls++
		}
	}
	return len(data) == 0 || controls*100/len(data) < 2
}
