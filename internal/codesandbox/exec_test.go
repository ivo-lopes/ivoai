//go:build linux && (amd64 || arm64)

package codesandbox

import (
	"context"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"
)

func TestHelper(t *testing.T) {
	if os.Getenv("CODESANDBOX_HELPER") != "1" {
		return
	}
	if os.Getenv("CODESANDBOX_UNAVAILABLE") == "1" {
		// Simulate a kernel without Landlock without weakening the production API.
		f := []unix.SockFilter{{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0}, {Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.SYS_LANDLOCK_CREATE_RULESET, Jf: 1}, {Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.ENOSYS)}, {Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW}}
		if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
			t.Fatal(err)
		}
		prog := unix.SockFprog{Len: uint16(len(f)), Filter: &f[0]}
		if _, _, e := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, 0, uintptr(unsafe.Pointer(&prog))); e != 0 {
			t.Fatal(e)
		}
		if err := Exec("/does-not-exist", nil, nil, nil); err == nil || !strings.Contains(err.Error(), "Landlock ABI") {
			t.Fatal("expected fail closed")
		}
		fmt.Println("FAIL_CLOSED")
		return
	}
	root := os.Getenv("CODESANDBOX_ROOT")
	err := Exec(os.Getenv("CODESANDBOX_NODE"), []string{"--openssl-config=/dev/null", "-e", os.Getenv("CODESANDBOX_SCRIPT")}, []string{filepath.Join(root, "read"), "/lib", "/lib64", "/usr/lib", "/dev/null", "/dev/urandom"}, []string{filepath.Join(root, "write")})
	if err != nil {
		t.Fatal(err)
	}
}

func TestNodeConfinement(t *testing.T) {
	node := os.Getenv("CODESANDBOX_TEST_NODE")
	if node == "" {
		t.Skip("set CODESANDBOX_TEST_NODE to the reviewed Node executable")
	}
	root := t.TempDir()
	for _, d := range []string{"read", "write", "outside"} {
		if err := os.Mkdir(filepath.Join(root, d), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{"read", "outside"} {
		if err := os.WriteFile(filepath.Join(root, d, "secret"), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "outside", "secret"), filepath.Join(root, "read", "escape")); err != nil {
		t.Fatal(err)
	}
	script := `const fs=require('fs'), assert=require('assert');const r=process.env.CODESANDBOX_ROOT;
 assert.equal(fs.readFileSync(r+'/read/secret','utf8'),'fixture');fs.writeFileSync(r+'/write/result','ok');
 for(const p of [r+'/outside/secret',r+'/read/escape']) assert.throws(()=>fs.readFileSync(p),e=>e.code==='EACCES');
 for(const p of [r+'/read/secret',r+'/outside/new']) assert.throws(()=>fs.writeFileSync(p,'bad'),e=>e.code==='EACCES');
 const {Worker}=require('worker_threads');const worker=new Worker('require("worker_threads").parentPort.postMessage(42)',{eval:true});worker.on('message',n=>assert.equal(n,42));
 const udp=require('dgram').createSocket('udp4');udp.on('error',e=>{assert.equal(e.code,'EPERM');udp.close()});udp.bind(0);
 const child=require('child_process').spawnSync(process.execPath,['-e','process.exit(0)']);assert(child.error);
 const net=require('net');const s=net.connect({host:'127.0.0.1',port:9});s.on('connect',()=>process.exit(31));s.on('error',e=>{assert.equal(e.code,'EPERM');console.log('CONFINED')});`
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHelper$")
	cmd.Env = append(os.Environ(), "CODESANDBOX_HELPER=1", "CODESANDBOX_ROOT="+root, "CODESANDBOX_NODE="+node, "CODESANDBOX_SCRIPT="+script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sandbox failed: %v: %s", err, out)
	}
	if string(out) != "CONFINED\n" {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestUnavailableFailsClosed(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root execution is rejected before probing Landlock")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHelper$")
	cmd.Env = append(os.Environ(), "CODESANDBOX_HELPER=1", "CODESANDBOX_UNAVAILABLE=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper: %v %s", err, out)
	}
	if string(out) != "FAIL_CLOSED\nPASS\n" {
		t.Fatalf("unexpected output: %s", out)
	}
}
