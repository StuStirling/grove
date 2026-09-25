//go:build darwin

package main

// Wails' file dialogs use UTType. The wails CLI links this framework itself;
// plain `go build -tags desktop,production` (CI) needs it spelled out.

/*
#cgo LDFLAGS: -framework UniformTypeIdentifiers
*/
import "C"
