package web

import (
	"embed"
	"io/fs"
)

//go:embed dist/*
var assets embed.FS

var Assets fs.FS

func init() {
	var err error
	Assets, err = fs.Sub(assets, "dist")
	if err != nil {
		panic(err)
	}
}
