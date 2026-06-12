// Package web 内嵌前端构建产物（dist/）。
package web

import "embed"

// Dist 前端构建产物。构建前端：cd web && pnpm build（输出到 web/dist）。
//
//go:embed all:dist
var Dist embed.FS
