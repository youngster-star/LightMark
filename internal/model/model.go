package model

// RecordType 记录类型
type RecordType string

const (
	// TypeFile 本地文件类型
	TypeFile RecordType = "file"
	// TypeURL 网页类型
	TypeURL RecordType = "url"
)

// Record 一条文件/网页标记记录
type Record struct {
	ID         string     `json:"id"`
	Type       RecordType `json:"type"`
	PathOrURL  string     `json:"pathOrUrl"`
	Title      string     `json:"title"`
	Note       string     `json:"note"`
	CategoryID string     `json:"categoryId"`
	Star       bool       `json:"star"`
	Invalid    bool       `json:"invalid"`
	Deleted    bool       `json:"deleted"`
	CreatedAt  int64      `json:"createdAt"`
	UpdatedAt  int64      `json:"updatedAt"`
	Favicon    string     `json:"favicon"`  // 网页 favicon 的 data URL（空表示未缓存）
	OpenedAt   int64      `json:"openedAt"` // 最近打开时间戳（毫秒，0 表示未打开过）
	OpenCount  int        `json:"openCount"`
	Tags       []Tag      `json:"tags"`
}

// Tag 标签
type Tag struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Category 分类（支持树形）
type Category struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ParentID string `json:"parentId"`
	Sort     int    `json:"sort"`
}

// RecordInput 创建/更新记录的入参
type RecordInput struct {
	ID         string   `json:"id"`
	Type       string   `json:"type"`
	PathOrURL  string   `json:"pathOrUrl"`
	Title      string   `json:"title"`
	Note       string   `json:"note"`
	CategoryID string   `json:"categoryId"`
	Star       bool     `json:"star"`
	Favicon    string   `json:"favicon"` // favicon data URL；更新时为空表示保持不变
	Tags       []string `json:"tags"`
}

// ListFilter 列表/搜索/筛选条件
type ListFilter struct {
	Type       string `json:"type"`       // "" | "file" | "url"
	CategoryID string `json:"categoryId"` // 指定分类
	Tag        string `json:"tag"`        // 指定标签名
	StarOnly   bool   `json:"starOnly"`   // 只看星标
	Trashed    bool   `json:"trashed"`    // 回收站
	Query      string `json:"query"`      // 搜索关键词
	SortBy     string `json:"sortBy"`     // updated_at | created_at | title | opened_at
	SortDesc   bool   `json:"sortDesc"`   // 是否降序
}

// Backup 备份数据结构，用于导入导出。
type Backup struct {
	Version    int        `json:"version"`
	ExportedAt int64      `json:"exportedAt"`
	Categories []Category `json:"categories"`
	Tags       []Tag      `json:"tags"`
	Records    []Record   `json:"records"`
}

// ImportResult 导入结果统计。
type ImportResult struct {
	Records    int `json:"records"`
	Categories int `json:"categories"`
	Tags       int `json:"tags"`
}
