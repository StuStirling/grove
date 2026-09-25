package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// The GUI listens on a per-repo unix socket (Config.socketPath). The CLI uses it
// to reach a running window (single instance, `grove open/new/remove`), and pane
// hooks use it to report Claude Code state (`grove state`). One JSON request and
// one JSON response per connection.

// ipcReq is a request to a running GUI.
type ipcReq struct {
	Op    string `json:"op"`              // ping | focus | open | close | state
	Name  string `json:"name,omitempty"`  // workspace, for focus/open/close
	Setup bool   `json:"setup,omitempty"` // open: fresh worktree, run the repo's setup
	Pane  string `json:"pane,omitempty"`  // state: reporting pane (GROVE_PANE)
	State string `json:"state,omitempty"` // state: working | waiting | idle | "" (clear)
}

type ipcResp struct {
	Err string `json:"err,omitempty"`
}

// errNotRunning means no GUI is listening on the socket.
var errNotRunning = errors.New("grove is not running")

// ipcCall sends one request. It returns errNotRunning when nothing listens, or
// the GUI's error for the request.
func ipcCall(sock string, req ipcReq) error {
	conn, err := net.DialTimeout("unix", sock, time.Second)
	if err != nil {
		return errNotRunning
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return err
	}
	var resp ipcResp
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return fmt.Errorf("reading grove reply: %w", err)
	}
	if resp.Err != "" {
		return errors.New(resp.Err)
	}
	return nil
}

// ipcServe listens on sock and answers each request with handle. It fails if a
// GUI is already listening there; a stale socket file is replaced.
func ipcServe(sock string, handle func(ipcReq) error) (net.Listener, error) {
	if ipcCall(sock, ipcReq{Op: "ping"}) == nil {
		return nil, fmt.Errorf("grove is already running for this repo")
	}
	if err := os.MkdirAll(filepath.Dir(sock), 0o700); err != nil {
		return nil, err
	}
	_ = os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return nil, err
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			go func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
				var req ipcReq
				if err := json.NewDecoder(conn).Decode(&req); err != nil {
					return
				}
				var resp ipcResp
				if err := handle(req); err != nil {
					resp.Err = err.Error()
				}
				_ = json.NewEncoder(conn).Encode(resp)
			}()
		}
	}()
	return ln, nil
}
