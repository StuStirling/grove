package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestIPC(t *testing.T) {
	// A short dir: unix socket paths are capped near 104 bytes.
	dir, err := os.MkdirTemp("/tmp", "grove")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "s.sock")

	if err := ipcCall(sock, ipcReq{Op: "ping"}); !errors.Is(err, errNotRunning) {
		t.Fatalf("no listener: err = %v, want errNotRunning", err)
	}

	// A stale socket file from a crashed GUI must not block a new one.
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var got ipcReq
	ln, err := ipcServe(sock, func(r ipcReq) error {
		got = r
		if r.Op == "open" {
			return errors.New("no workspace named \"x\"")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	if err := ipcCall(sock, ipcReq{Op: "state", Pane: "p1", State: "idle"}); err != nil {
		t.Fatal(err)
	}
	if got.Op != "state" || got.Pane != "p1" || got.State != "idle" {
		t.Fatalf("handler got %+v", got)
	}
	if err := ipcCall(sock, ipcReq{Op: "open", Name: "x"}); err == nil || errors.Is(err, errNotRunning) {
		t.Fatalf("handler error not relayed: %v", err)
	}
	if _, err := ipcServe(sock, func(ipcReq) error { return nil }); err == nil {
		t.Fatal("second instance on the same socket should fail")
	}
}
