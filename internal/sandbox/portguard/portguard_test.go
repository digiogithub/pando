package portguard

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

func reset(t *testing.T) {
	t.Helper()
	ResetForTests()
	t.Cleanup(ResetForTests)
}

func TestRegisterUnregister(t *testing.T) {
	reset(t)
	t.Cleanup(SetSharedDirForTests(""))
	u1 := Register(8765, "api")
	u2 := Register(8765, "api-rebind")
	u3 := Register(20001, "ipc-rpc")
	Register(0, "bad")
	Register(70000, "bad")
	if got := Ports(); !reflect.DeepEqual(got, []int{8765, 20001}) {
		t.Fatalf("Ports = %v", got)
	}
	u1()
	u1() // idempotent
	if got := Ports(); !reflect.DeepEqual(got, []int{8765, 20001}) {
		t.Fatalf("after one of two 8765 registrations removed: %v", got)
	}
	u2()
	u3()
	if got := Ports(); len(got) != 0 {
		t.Fatalf("after unregister: %v", got)
	}
}

func TestGuardListener(t *testing.T) {
	reset(t)
	t.Cleanup(SetSharedDirForTests(""))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	g := Guard(ln, "test")
	if got := LocalPorts(); !reflect.DeepEqual(got, []int{port}) {
		t.Fatalf("LocalPorts = %v, want [%d]", got, port)
	}
	if owners := LocalOwners()[port]; !reflect.DeepEqual(owners, []string{"test"}) {
		t.Fatalf("owners = %v", owners)
	}
	if g.Addr().String() != ln.Addr().String() {
		t.Fatal("Addr not forwarded")
	}
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	if got := LocalPorts(); len(got) != 0 {
		t.Fatalf("Close did not unregister: %v", got)
	}
}

// TestSharedRegistry: this process publishes its ports, and reads the ports
// of other live processes while dropping the files of dead ones.
func TestSharedRegistry(t *testing.T) {
	reset(t)
	dir := t.TempDir()
	t.Cleanup(SetSharedDirForTests(dir))

	unregister := Register(18765, "api")
	self := filepath.Join(dir, strconv.Itoa(os.Getpid())+".json")
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatalf("own registry file: %v", err)
	}
	var f sharedFile
	if err := json.Unmarshal(data, &f); err != nil || !reflect.DeepEqual(f.Ports, []int{18765}) {
		t.Fatalf("own file = %s (%v)", data, err)
	}

	write := func(pid int, ports ...int) string {
		path := filepath.Join(dir, strconv.Itoa(pid)+".json")
		b, _ := json.Marshal(sharedFile{PID: pid, Ports: ports})
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	write(os.Getppid(), 9767, 20000) // the test runner: alive
	dead := write(1<<22+12345, 4444) // above pid_max: not a live process

	if got := Ports(); !reflect.DeepEqual(got, []int{9767, 18765, 20000}) {
		t.Fatalf("Ports = %v", got)
	}
	if _, err := os.Stat(dead); !os.IsNotExist(err) {
		t.Fatalf("dead process file not removed: %v", err)
	}

	unregister()
	if _, err := os.Stat(self); !os.IsNotExist(err) {
		t.Fatalf("own file not removed after the last unregister: %v", err)
	}
}
