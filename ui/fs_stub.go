package ui

import "io/fs"

type emptyFS struct{}

func (emptyFS) Open(string) (fs.File, error) {
	return nil, fs.ErrNotExist
}

// FS is a temporary placeholder until WebUI assets are embedded in T3.8.
var FS fs.FS = emptyFS{}
