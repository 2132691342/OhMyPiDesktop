// 内置资源：用户手册随二进制一起发行，断网也能看。
package main

import "embed"

// docsFS 是 assets/docs 下的手册。用 all: 是为了把非 .go 文件也打进来。
//
//go:embed all:assets/docs
var docsFS embed.FS
