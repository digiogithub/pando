// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package ipc

import (
	"context"
	"net"
	"testing"
	"time"
)

// freePort returns a loopback port that is free right now.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// The recorded ports are kept, and the lock file left alone, when they are free.
func TestClaimLeaderPortsKeepsFreePorts(t *testing.T) {
	pub, rpc := freePort(t), freePort(t)
	workdir := t.TempDir()
	isPrimary, _, lockFile, err := AcquireLock(workdir, "leader", pub, rpc)
	if err != nil || !isPrimary {
		t.Fatalf("AcquireLock: primary=%v err=%v", isPrimary, err)
	}
	defer ReleaseLock(lockFile)

	gotPub, gotRPC := ClaimLeaderPorts(context.Background(), lockFile, pub, rpc)
	if gotPub != pub || gotRPC != rpc {
		t.Fatalf("ClaimLeaderPorts = %d/%d, want the free recorded ports %d/%d", gotPub, gotRPC, pub, rpc)
	}
	info, err := ReadLockForPath(workdir)
	if err != nil {
		t.Fatal(err)
	}
	if info.PubPort != pub || info.RPCPort != rpc {
		t.Fatalf("lock file ports = %d/%d, want %d/%d", info.PubPort, info.RPCPort, pub, rpc)
	}
}

// A recorded port held by an unrelated process must not leave the leader
// unreachable: it moves to free ports and records them in the lock file (the
// place followers and CLI tools read them from), keeping the other fields.
func TestClaimLeaderPortsFallsBackWhenTaken(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	pub := taken.Addr().(*net.TCPAddr).Port
	rpc := freePort(t)

	workdir := t.TempDir()
	isPrimary, before, lockFile, err := AcquireLock(workdir, "leader", pub, rpc)
	if err != nil || !isPrimary {
		t.Fatalf("AcquireLock: primary=%v err=%v", isPrimary, err)
	}
	defer ReleaseLock(lockFile)

	gotPub, gotRPC := ClaimLeaderPorts(context.Background(), lockFile, pub, rpc)
	if gotPub == pub {
		t.Fatalf("ClaimLeaderPorts kept the taken PUB port %d", pub)
	}
	if gotPub <= 0 || gotRPC <= 0 || gotPub == gotRPC {
		t.Fatalf("ClaimLeaderPorts = %d/%d, want two distinct ports", gotPub, gotRPC)
	}

	info, err := ReadLockForPath(workdir)
	if err != nil {
		t.Fatal(err)
	}
	if info.PubPort != gotPub || info.RPCPort != gotRPC {
		t.Fatalf("lock file ports = %d/%d, want the claimed %d/%d", info.PubPort, info.RPCPort, gotPub, gotRPC)
	}
	if info.InstanceID != before.InstanceID || info.PID != before.PID || !info.StartedAt.Equal(before.StartedAt) {
		t.Fatalf("lock file identity changed: %+v, want %+v", info, before)
	}

	// The claimed ports must be bindable by the bus.
	bus := NewBus("claim-test")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := StartBusWithRetry(ctx, bus, gotPub, gotRPC); err != nil {
		t.Fatalf("bus on the claimed ports: %v", err)
	}
	_ = bus.Shutdown()
}

// A stop during startup must not wait out the retry window.
func TestClaimLeaderPortsHonoursContext(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	pub := taken.Addr().(*net.TCPAddr).Port

	workdir := t.TempDir()
	isPrimary, _, lockFile, err := AcquireLock(workdir, "leader", pub, pub+1)
	if err != nil || !isPrimary {
		t.Fatalf("AcquireLock: primary=%v err=%v", isPrimary, err)
	}
	defer ReleaseLock(lockFile)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	gotPub, _ := ClaimLeaderPorts(ctx, lockFile, pub, pub+1)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("ClaimLeaderPorts with a cancelled context took %s", elapsed)
	}
	if gotPub != pub {
		t.Fatalf("ClaimLeaderPorts with a cancelled context moved to %d", gotPub)
	}
}

func TestFindFreePortsDistinct(t *testing.T) {
	for i := 0; i < 20; i++ {
		pub, rpc, err := FindFreePorts()
		if err != nil {
			t.Fatal(err)
		}
		if pub == rpc {
			t.Fatalf("FindFreePorts returned the same port twice: %d", pub)
		}
	}
}
