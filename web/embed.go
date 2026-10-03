// Package web 在生产构建时嵌入真实前端产物；必须先构建前端，不提供占位资源。
package web

import (
	"embed"
	"io/fs"
)

//go:embed dist
var assets embed.FS

func Assets() (fs.FS, error) { return fs.Sub(assets, "dist") }
