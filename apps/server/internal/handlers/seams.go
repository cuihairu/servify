package handlers

// 本文件聚合仅供测试注入的包级 seam。每个变量的默认值都保持生产行为，
// 生产代码不得在运行时改写；测试通过替换变量来驱动错误分支。

import (
	"crypto/rand"
	"encoding/csv"
	"io"

	"github.com/xuri/excelize/v2"
)

// newCSVWriter 供 CSV 导出（audit / tickets / statistics）注入写失败的
// writer，以驱动行写入与 Flush 的错误分支。
var newCSVWriter = csv.NewWriter

// oidcRandRead 供 OIDC state/nonce 生成路径注入 crypto/rand.Read 失败。
var oidcRandRead = rand.Read

// XLSX 导出（statistics）的 excelize 注入点：excelize 对固定 "Sheet1"/
// 合法单元格的操作在真实实现下不会失败，测试通过替换这些变量驱动错误分支。
var (
	hookExcelizeSetSheetRow = func(f *excelize.File, sheet, cell string, slice interface{}) error {
		return f.SetSheetRow(sheet, cell, slice)
	}
	hookExcelizeCellName = func(col, row int, abs ...bool) (string, error) {
		return excelize.CoordinatesToCellName(col, row, abs...)
	}
	hookExcelizeWrite = func(f *excelize.File, w io.Writer) error { return f.Write(w) }
)
