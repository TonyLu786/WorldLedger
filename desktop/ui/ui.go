// Package ui is the page, compiled into the binary.
//
// Embedding rather than shipping a folder is what makes the application one
// file. It also means the page cannot be edited into something else between
// download and run, and that there is no directory for a stray copy of an
// older version to sit in.
package ui

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/worldledger/worldledger-mc/desktop/internal/app"
)

//go:embed assets
var assets embed.FS

// Mount serves the page at the root.
func Mount(server *app.Server) error {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		return err
	}
	server.Handle("/", http.FileServer(http.FS(sub)))
	return nil
}
