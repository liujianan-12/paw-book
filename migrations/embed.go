// Package migrations 内嵌顺序迁移脚本（按文件名排序执行，可重复执行）。
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
