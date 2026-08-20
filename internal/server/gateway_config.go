package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const gatewayConfigFile = "gateway.json"

// GatewayConfig controls the public gateway without requiring users to edit
// systemd units or configuration files. A loopback listener without TLS is
// suitable for a local TLS-terminating reverse proxy. A non-loopback listener
// must use direct TLS or restrict plaintext traffic to explicit proxy CIDRs.
type GatewayConfig struct {
	ListenAddress     string   `json:"listen_address"`
	PublicURL         string   `json:"public_url,omitempty"`
	TLSCertFile       string   `json:"tls_cert_file,omitempty"`
	TLSKeyFile        string   `json:"tls_key_file,omitempty"`
	TrustedProxyCIDRs []string `json:"trusted_proxy_cidrs,omitempty"`
}

func DefaultGatewayConfig() GatewayConfig {
	return GatewayConfig{ListenAddress: "127.0.0.1:7744"}
}

func GatewayConfigPath(layout Layout) string {
	return filepath.Join(layout.ConfigDir, gatewayConfigFile)
}

// InstallGatewayTLS copies explicitly selected certificate material into the
// ivoai-managed configuration tree. This lets the unprivileged gateway read
// direct-TLS material without changing ownership of files managed by certbot
// or another issuer. Re-running configure refreshes the managed copy.
func InstallGatewayTLS(layout Layout, certificateSource, keySource string) (string, string, error) {
	certificate, err := readTLSFile(certificateSource, false)
	if err != nil {
		return "", "", err
	}
	key, err := readTLSFile(keySource, true)
	if err != nil {
		return "", "", err
	}
	// Direct-TLS key material belongs with server secrets, not root-owned
	// non-secret configuration. Server setup assigns this tree to the dedicated
	// service account while retaining owner-only modes.
	directory := filepath.Join(layout.SecretsDir, "tls")
	if info, err := os.Lstat(directory); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", "", errors.New("refusing symlink gateway TLS directory")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", "", err
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return "", "", err
	}
	certificatePath := filepath.Join(directory, "certificate.pem")
	keyPath := filepath.Join(directory, "private-key.pem")
	if err := writeManagedFile(certificatePath, certificate, 0o600); err != nil {
		return "", "", err
	}
	if err := writeManagedFile(keyPath, key, 0o600); err != nil {
		return "", "", err
	}
	return certificatePath, keyPath, nil
}

func readTLSFile(path string, private bool) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("TLS source paths must be absolute")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open TLS source: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open TLS source")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 2<<20 {
		return nil, errors.New("TLS source must be a non-empty regular file no larger than 2 MiB")
	}
	if private && info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("TLS key source permissions must be 0600")
	}
	data, err := io.ReadAll(io.LimitReader(file, (2<<20)+1))
	if err != nil {
		return nil, fmt.Errorf("read TLS source: %w", err)
	}
	if len(data) == 0 || len(data) > 2<<20 {
		return nil, errors.New("TLS source must be a non-empty regular file no larger than 2 MiB")
	}
	return data, nil
}

func LoadGatewayConfig(layout Layout) (GatewayConfig, error) {
	config := DefaultGatewayConfig()
	path := GatewayConfigPath(layout)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return config, nil
	}
	if err != nil {
		return config, err
	}
	permissions := info.Mode().Perm()
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || (permissions != 0o600 && permissions != 0o640) {
		return config, errors.New("gateway configuration must be a regular 0600 or root-owned 0640 file")
	}
	file, err := os.Open(path)
	if err != nil {
		return config, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return config, fmt.Errorf("decode gateway configuration: %w", err)
	}
	return config, config.Validate(false)
}

// SaveGatewayConfig validates and atomically persists a user-selected gateway
// endpoint. TLS file contents remain outside the config and are never logged.
func SaveGatewayConfig(layout Layout, config GatewayConfig) error {
	if err := config.Validate(true); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return writeManagedFile(GatewayConfigPath(layout), append(encoded, '\n'), 0o600)
}

func (c GatewayConfig) Validate(checkTLSFiles bool) error {
	host, _, err := net.SplitHostPort(c.ListenAddress)
	if err != nil {
		return fmt.Errorf("listen address must be host:port: %w", err)
	}
	if c.PublicURL != "" {
		public, err := url.Parse(c.PublicURL)
		if err != nil || public.Scheme != "https" || public.Host == "" || public.User != nil || public.RawQuery != "" || public.Fragment != "" {
			return errors.New("public URL must be an HTTPS origin without credentials, query, or fragment")
		}
		if public.Path != "" && public.Path != "/" {
			return errors.New("public URL must not contain a path")
		}
	}
	if (c.TLSCertFile == "") != (c.TLSKeyFile == "") {
		return errors.New("TLS certificate and key must be configured together")
	}
	if c.TLSCertFile != "" && len(c.TrustedProxyCIDRs) > 0 {
		return errors.New("direct TLS and trusted reverse-proxy modes are mutually exclusive")
	}
	loopback := strings.EqualFold(host, "localhost")
	if ip := net.ParseIP(host); ip != nil {
		loopback = ip.IsLoopback()
	}
	if host == "" {
		loopback = false
	}
	if !loopback && c.TLSCertFile == "" && len(c.TrustedProxyCIDRs) == 0 {
		return errors.New("a non-loopback gateway listener requires direct TLS or at least one explicitly trusted HTTPS reverse-proxy CIDR")
	}
	if len(c.TrustedProxyCIDRs) > 0 {
		if c.PublicURL == "" {
			return errors.New("trusted reverse-proxy mode requires a public HTTPS URL")
		}
		if _, err := ParseTrustedProxyCIDRs(c.TrustedProxyCIDRs); err != nil {
			return err
		}
	}
	if !checkTLSFiles || c.TLSCertFile == "" {
		return nil
	}
	for _, item := range []struct {
		name string
		path string
		key  bool
	}{{"TLS certificate", c.TLSCertFile, false}, {"TLS key", c.TLSKeyFile, true}} {
		if !filepath.IsAbs(item.path) {
			return fmt.Errorf("%s path must be absolute", item.name)
		}
		info, err := os.Lstat(item.path)
		if err != nil {
			return fmt.Errorf("%s: %w", item.name, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("%s must be a regular non-symlink file", item.name)
		}
		if item.key && info.Mode().Perm()&0o077 != 0 {
			return errors.New("TLS key permissions must be 0600")
		}
	}
	return nil
}

// ParseTrustedProxyCIDRs validates source networks used by the plaintext
// reverse-proxy listener. A wildcard network would turn this mode into a
// public HTTP endpoint and is therefore rejected.
func ParseTrustedProxyCIDRs(values []string) ([]*net.IPNet, error) {
	networks := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		ip, network, err := net.ParseCIDR(value)
		if err != nil || ip == nil || network == nil {
			return nil, fmt.Errorf("trusted proxy must be an IP CIDR: %q", value)
		}
		ones, bits := network.Mask.Size()
		if ones <= 0 || bits <= 0 {
			return nil, errors.New("trusted proxy CIDR must not match every address")
		}
		networks = append(networks, network)
	}
	return networks, nil
}
