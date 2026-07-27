package ui

type ResultItem struct {
	FullPath string // 文件或文件夹的完整路径
	FileName string // 纯文件名
	Path     string // 纯目录路径
	Size     int64  // 文件大小（字节，如果是文件夹则通常为0）
	IsFolder bool   // 是否是文件夹
}
