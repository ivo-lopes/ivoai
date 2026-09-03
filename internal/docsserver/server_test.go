package docsserver

import (
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestEmbeddedProductionSiteAndHealth(t *testing.T) {
	handler, err := Handler()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	if host, _, _ := net.SplitHostPort(listener.Addr().String()); host != "0.0.0.0" {
		t.Fatalf("listener=%s", listener.Addr())
	}
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })
	base := "http://127.0.0.1:" + strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	for _, route := range []string{"/", "/docs/quickstart", "/healthz"} {
		response, err := client.Get(base + route)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK || len(body) == 0 {
			t.Fatalf("route=%s status=%d body=%q", route, response.StatusCode, body)
		}
		if response.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("security headers missing for %s", route)
		}
	}
	lanIP := firstNonLoopbackIPv4(t)
	response, err := client.Get("http://" + net.JoinHostPort(lanIP, strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)) + "/healthz")
	if err != nil {
		t.Fatalf("LAN probe through wildcard listener: %v", err)
	}
	lanBody, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(string(lanBody), "healthy") {
		t.Fatalf("LAN probe status=%d body=%q", response.StatusCode, lanBody)
	}
	response, err = client.Get(base + "/does-not-exist")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown route status=%d", response.StatusCode)
	}
	index, _ := io.ReadAll(mustOpen(t, "site/index.html"))
	if !strings.Contains(string(index), "IVOAI") {
		t.Fatal("embedded output is not the Docusaurus build")
	}
}

func firstNonLoopbackIPv4(t *testing.T) string {
	t.Helper()
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	for _, address := range addresses {
		ip, _, err := net.ParseCIDR(address.String())
		if err == nil && ip.To4() != nil && !ip.IsLoopback() {
			return ip.String()
		}
	}
	t.Fatal("no non-loopback IPv4 address available for LAN listener test")
	return ""
}

func mustOpen(t *testing.T, name string) io.ReadCloser {
	t.Helper()
	file, err := embedded.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	return file
}
