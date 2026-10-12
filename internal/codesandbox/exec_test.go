//go:build linux && (amd64 || arm64)

package codesandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
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
	if os.Getenv("CODESANDBOX_SYSCALLS") == "1" {
		runtime.LockOSThread()
		if e := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); e != nil {
			t.Fatal(e)
		}
		if e := restrictSyscalls(); e != nil {
			t.Fatal(e)
		}
		root := os.Getenv("CODESANDBOX_ROOT")
		if e := unix.Truncate(root, 0); e != unix.EPERM {
			t.Fatalf("truncate expected EPERM, got %v", e)
		}
		fd, e := unix.Open(root, unix.O_RDONLY|unix.O_TRUNC, 0)
		if fd >= 0 {
			unix.Close(fd)
		}
		if e != unix.EPERM {
			t.Fatalf("read-only O_TRUNC expected EPERM, got %v", e)
		}
		fd, e = unix.Open(root, unix.O_WRONLY, 0)
		if e != nil {
			t.Fatal(e)
		}
		if e = unix.Ftruncate(fd, 0); e != unix.EPERM {
			t.Fatalf("ftruncate: %v", e)
		}
		unix.Close(fd)
		for _, flags := range []int{unix.O_TRUNC | unix.O_ACCMODE, unix.O_TRUNC} {
			fd, e = unix.Openat(unix.AT_FDCWD, root, flags, 0)
			if fd >= 0 {
				unix.Close(fd)
			}
			if e != unix.EPERM {
				t.Fatalf("openat flags %x: %v", flags, e)
			}
		}
		fd, e = unix.Openat2(unix.AT_FDCWD, root, &unix.OpenHow{Flags: unix.O_RDONLY})
		if fd >= 0 {
			unix.Close(fd)
		}
		if e != unix.ENOSYS {
			t.Fatalf("openat2: %v", e)
		}
		if runtime.GOARCH == "amd64" {
			path, e := unix.BytePtrFromString(root)
			if e != nil {
				t.Fatal(e)
			}
			_, _, errno := unix.Syscall(2, uintptr(unsafe.Pointer(path)), unix.O_TRUNC, 0)
			if errno != unix.EPERM {
				t.Fatalf("legacy open: %v", errno)
			}
		}
		if e := unix.Tgkill(os.Getppid(), os.Getppid(), 0); e != unix.EPERM {
			t.Fatalf("foreign tgkill expected EPERM, got %v", e)
		}
		if e := unix.Tgkill(os.Getpid(), unix.Gettid(), 0); e != nil {
			t.Fatal(e)
		}
		fmt.Println("SYSCALLS_CONFINED")
		return
	}
	root := os.Getenv("CODESANDBOX_ROOT")
	ceiling, _ := strconv.ParseUint(os.Getenv("CODESANDBOX_ABI"), 10, 64)
	err := execWithABICeiling(os.Getenv("CODESANDBOX_NODE"), []string{"--openssl-config=/dev/null", "-e", os.Getenv("CODESANDBOX_SCRIPT")}, []string{filepath.Join(root, "read"), "/lib", "/lib64", "/usr/lib", "/dev/null", "/dev/urandom"}, []string{filepath.Join(root, "write")}, uintptr(ceiling))
	if err != nil {
		t.Fatal(err)
	}
}

func TestNodeConfinement(t *testing.T) {
	for _, abi := range []string{"0", "1", "2"} {
		t.Run("ABI"+abi, func(t *testing.T) { testNodeConfinement(t, abi) })
	}
}
func testNodeConfinement(t *testing.T, abi string) {
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
 assert.equal(fs.readFileSync(r+'/read/secret','utf8'),'fixture');fs.writeFileSync(r+'/write/result','old-content');fs.writeFileSync(r+'/write/result','ok');assert.equal(fs.readFileSync(r+'/write/result','utf8'),'ok');
 fs.writeFileSync(r+'/write/atomic.tmp','renamed');fs.renameSync(r+'/write/atomic.tmp',r+'/write/atomic');
 assert.throws(()=>fs.openSync(r+'/read/secret',fs.constants.O_RDONLY|fs.constants.O_TRUNC),e=>e.code==='EPERM');
 assert.equal(fs.readFileSync(r+'/read/secret','utf8'),'fixture');
 for(const p of [r+'/outside/secret',r+'/read/escape']) assert.throws(()=>fs.readFileSync(p),e=>e.code==='EACCES');
 for(const p of [r+'/read/secret',r+'/outside/new']) assert.throws(()=>fs.writeFileSync(p,'bad'),e=>e.code==='EACCES');
 const {Worker}=require('worker_threads');const worker=new Worker('require("worker_threads").parentPort.postMessage(42)',{eval:true});worker.on('message',n=>assert.equal(n,42));
 const udp=require('dgram').createSocket('udp4');udp.on('error',e=>{assert.equal(e.code,'EPERM');udp.close()});udp.bind(0);
 const child=require('child_process').spawnSync(process.execPath,['-e','process.exit(0)']);assert(child.error);
 const net=require('net');const s=net.connect({host:'127.0.0.1',port:9});s.on('connect',()=>process.exit(31));s.on('error',e=>{assert.equal(e.code,'EPERM');console.log('CONFINED')});`
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHelper$")
	cmd.Env = append(os.Environ(), "CODESANDBOX_HELPER=1", "CODESANDBOX_ROOT="+root, "CODESANDBOX_NODE="+node, "CODESANDBOX_SCRIPT="+script, "CODESANDBOX_ABI="+abi)
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

func TestCompensatingSyscalls(t *testing.T) {
	root := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(root, []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHelper$")
	cmd.Env = append(os.Environ(), "CODESANDBOX_HELPER=1", "CODESANDBOX_SYSCALLS=1", "CODESANDBOX_ROOT="+root)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper: %v %s", err, out)
	}
	if string(out) != "SYSCALLS_CONFINED\nPASS\n" {
		t.Fatalf("unexpected output: %s", out)
	}
	data, err := os.ReadFile(root)
	if err != nil || string(data) != "retained" {
		t.Fatalf("fixture modified: %v", err)
	}
}
