package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"fyne.io/systray"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"light_mark/internal/api"
	"light_mark/internal/autostart"
	"light_mark/internal/fileops"
	"light_mark/internal/hotkey"
	"light_mark/internal/model"
	"light_mark/internal/store"
)

// App 应用门面，暴露给前端的绑定方法都定义在此。
type App struct {
	ctx       context.Context
	store     *store.Store
	apiServer *api.Server
	hotkey    *hotkey.Handle
	trayOn    bool // 托盘是否已启动（决定 shutdown 是否需要 Quit）

	// appCtx 应用级后台任务生命周期：shutdown 时取消，终止校验/备份循环
	appCtx    context.Context
	appCancel context.CancelFunc

	// validateMu 校验互斥：防止启动自动校验与手动校验并发重复执行
	validateMu sync.Mutex

	// 撤销栈（单条）：记录最近一次变更性记录操作的前状态
	undoMu   sync.Mutex
	undoLast *undoEntry
}

// undoEntry 撤销条目：kind 为操作类型，rec 为操作前完整快照。
type undoEntry struct {
	kind string        // delete | restore | create | update
	rec  *model.Record // 操作前快照（含标签）
	desc string        // 面向用户的描述
}

// NewApp 创建 App 实例。
func NewApp() *App {
	return &App{}
}

// startup 应用启动时调用，初始化数据存储。
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// 后台任务生命周期 context：必须在任何 return 之前创建，保证 shutdown 可安全取消
	a.appCtx, a.appCancel = context.WithCancel(context.Background())
	dataDir, err := defaultDataDir()
	if err != nil {
		fmt.Printf("解析数据目录失败: %v\n", err)
		return
	}
	s, err := store.Open(dataDir)
	if err != nil {
		fmt.Printf("初始化存储失败: %v\n", err)
		return
	}
	a.store = s
	// 启动本地 API 服务桩（127.0.0.1 随机端口，AI 桌宠预留）
	srv, err := api.Start()
	if err != nil {
		fmt.Printf("本地 API 服务启动失败: %v\n", err)
	} else {
		a.apiServer = srv
	}
	// 注册 Alt+D 全局热键：任意界面下唤出主窗口（失败不阻断启动）
	h, err := hotkey.RegisterAltD(a.showMainWindow)
	if err != nil {
		fmt.Printf("注册全局热键 Alt+D 失败: %v\n", err)
	} else {
		a.hotkey = h
	}
	// 启动系统托盘（常驻后台）
	a.startTray()
	// 后台任务：启动时自动校验失效记录 + 周期自动备份（不阻塞启动）
	go func() {
		a.autoValidate()
		a.autoBackupLoop(dataDir)
	}()
}

// autoValidate 启动时全量校验失效记录，发现在后台落库并通知前端刷新。
func (a *App) autoValidate() {
	if a.store == nil {
		return
	}
	n, err := a.ValidateAllRecords()
	if err != nil {
		fmt.Printf("启动失效校验失败: %v\n", err)
		return
	}
	if n > 0 {
		runtime.EventsEmit(a.ctx, "records-invalidated", n)
	}
}

// autoBackupLoop 周期性自动备份：启动时先检查一次，之后每小时复查，
// 距上次备份超过 24 小时才导出，滚动仅保留最近 10 份。
// 长期不重启的应用也能持续获得自动备份（PRD：定时自动备份）。
func (a *App) autoBackupLoop(dataDir string) {
	a.checkBackupOnce(dataDir)
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-a.appCtx.Done():
			return
		case <-ticker.C:
			a.checkBackupOnce(dataDir)
		}
	}
}

// checkBackupOnce 执行一次「距上次备份是否超过 24 小时」的判断与导出。
func (a *App) checkBackupOnce(dataDir string) {
	backupDir := filepath.Join(dataDir, "backups")
	entries, err := os.ReadDir(backupDir)
	if err == nil {
		var latest time.Time
		hasBackup := false
		for _, e := range entries {
			if info, err := e.Info(); err == nil && info.ModTime().After(latest) {
				latest = info.ModTime()
				hasBackup = true
			}
		}
		// 距最近一次备份不足 24 小时则跳过
		if hasBackup && time.Since(latest) < 24*time.Hour {
			return
		}
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		fmt.Printf("创建备份目录失败: %v\n", err)
		return
	}
	name := "auto-" + time.Now().Format("20060102-150405") + ".json"
	path := filepath.Join(backupDir, name)
	if err := a.ExportToFile(path); err != nil {
		fmt.Printf("自动备份失败: %v\n", err)
		return
	}
	// 滚动清理：仅保留最近 10 份自动备份
	a.pruneBackups(backupDir, 10)
}

// pruneBackups 按修改时间删除最旧的备份文件，仅保留 keep 份。
func (a *App) pruneBackups(backupDir string, keep int) {
	files, err := os.ReadDir(backupDir)
	if err != nil || len(files) <= keep {
		return
	}
	type fileTime struct {
		name string
		mod  time.Time
	}
	list := make([]fileTime, 0, len(files))
	for _, f := range files {
		if info, err := f.Info(); err == nil {
			list = append(list, fileTime{f.Name(), info.ModTime()})
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].mod.Before(list[j].mod) })
	for i := 0; i < len(list)-keep; i++ {
		_ = os.Remove(filepath.Join(backupDir, list[i].name))
	}
}

// shutdown 应用退出时注销热键、停止服务并关闭存储。
func (a *App) shutdown(ctx context.Context) {
	// 先取消后台任务（校验/备份循环），再逐层释放资源
	if a.appCancel != nil {
		a.appCancel()
	}
	a.hotkey.Stop()
	if a.apiServer != nil {
		_ = a.apiServer.Stop()
	}
	// 托盘仅在成功启动过时退出，避免图标残留
	if a.trayOn {
		systray.Quit()
	}
	if a.store != nil {
		_ = a.store.Close()
	}
}

// showMainWindow 显示并聚焦主窗口，供托盘菜单与全局热键共用。
func (a *App) showMainWindow() {
	runtime.WindowUnminimise(a.ctx)
	runtime.WindowShow(a.ctx)
}

// defaultDataDir 返回数据目录：系统用户配置目录下的 light_mark。
func defaultDataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "light_mark"), nil
}

// ---------- 记录 ----------

// pushUndo 记录一笔可撤销操作前状态。
func (a *App) pushUndo(kind string, rec *model.Record, desc string) {
	a.undoMu.Lock()
	a.undoLast = &undoEntry{kind: kind, rec: rec, desc: desc}
	a.undoMu.Unlock()
}

// TryCreateRecord 带重复检测的录入：发现同路径/网址的已有记录时阻止并返回提示。
// 是否仍要保存由用户在界面上确认后调用 ForceCreateRecord。
// 错误消息以 "DUPLICATE|" 前缀标记重复类型，供前端程序化识别
// （避免依赖中文文案匹配，文案调整不会破坏强制保存流程）。
func (a *App) TryCreateRecord(in model.RecordInput) (*model.Record, error) {
	if dup, err := a.store.FindDuplicate(in.PathOrURL, ""); err != nil {
		return nil, err
	} else if dup != nil {
		return nil, fmt.Errorf("DUPLICATE|已存在相同%s的记录：%s",
			dupTypeWord(dup.Type), dup.Title)
	}
	return a.ForceCreateRecord(in)
}

// ForceCreateRecord 跳过重复检测强制创建（用户已确认）。
func (a *App) ForceCreateRecord(in model.RecordInput) (*model.Record, error) {
	rec, err := a.store.CreateRecord(in)
	if err == nil && rec != nil {
		a.pushUndo("create", rec, "新增: "+rec.Title)
	}
	return rec, err
}

// TryUpdateRecord 编辑记录：更新失败前先记账旧快照，供撤销恢复。
func (a *App) TryUpdateRecord(in model.RecordInput) (*model.Record, error) {
	old, err := a.store.GetRecord(in.ID)
	if err != nil {
		return nil, err
	}
	rec, err := a.store.UpdateRecord(in)
	if err == nil && rec != nil {
		a.pushUndo("update", old, "修改: "+rec.Title)
	}
	return rec, err
}

// TryDeleteRecord 软删除记录：先记下删除前快照，供撤销恢复。
func (a *App) TryDeleteRecord(id string) error {
	old, err := a.store.GetRecord(id)
	if err != nil {
		return err
	}
	if err := a.store.SoftDeleteRecord(id); err != nil {
		return err
	}
	a.pushUndo("delete", old, "删除: "+old.Title)
	return nil
}

// TryRestoreRecord 恢复回收站记录：记账恢复前快照，供撤销。
func (a *App) TryRestoreRecord(id string) error {
	old, err := a.store.GetRecord(id)
	if err != nil {
		return err
	}
	if err := a.store.RestoreRecord(id); err != nil {
		return err
	}
	a.pushUndo("restore", old, "恢复: "+old.Title)
	return nil
}

// UndoLastAction 撤销最近一次记录操作（新增/删除/恢复/修改）。
// 返回撤销描述；无操作可撤销时返回错误。
func (a *App) UndoLastAction() (string, error) {
	a.undoMu.Lock()
	entry := a.undoLast
	a.undoLast = nil
	a.undoMu.Unlock()
	if entry == nil {
		return "", fmt.Errorf("没有可撤销的操作")
	}
	switch entry.kind {
	case "delete": // 撤销删除 → 恢复
		if err := a.store.RestoreRecord(entry.rec.ID); err != nil {
			return "", err
		}
	case "restore", "update": // 撤销恢复/修改 → 还原为操作前快照
		if err := a.store.RestoreRecordSnapshot(entry.rec); err != nil {
			return "", err
		}
	case "create": // 撤销新增 → 移入回收站
		if err := a.store.SoftDeleteRecord(entry.rec.ID); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("未知操作类型: %s", entry.kind)
	}
	return "已撤销：" + entry.desc, nil
}

// dupTypeWord 重复提示中的类型用词。
func dupTypeWord(t model.RecordType) string {
	if t == model.TypeURL {
		return "网址"
	}
	return "文件路径"
}

// ListRecords 按条件查询记录列表。
func (a *App) ListRecords(f model.ListFilter) ([]model.Record, error) {
	return a.store.ListRecords(f)
}

// GetRecord 读取单条记录。
func (a *App) GetRecord(id string) (*model.Record, error) {
	return a.store.GetRecord(id)
}

// CreateRecord 创建记录（带重复检测）。
func (a *App) CreateRecord(in model.RecordInput) (*model.Record, error) {
	return a.TryCreateRecord(in)
}

// UpdateRecord 更新记录（撤销可还原）。
func (a *App) UpdateRecord(in model.RecordInput) (*model.Record, error) {
	return a.TryUpdateRecord(in)
}

// DeleteRecord 软删除记录（移入回收站，可撤销）。
func (a *App) DeleteRecord(id string) error {
	return a.TryDeleteRecord(id)
}

// RestoreRecord 从回收站恢复记录（可撤销）。
func (a *App) RestoreRecord(id string) error {
	return a.TryRestoreRecord(id)
}

// PurgeRecord 彻底删除记录。
func (a *App) PurgeRecord(id string) error {
	return a.store.PurgeRecord(id)
}

// OpenRecord 读取记录并用本机默认应用打开其文件或网址，同时记录最近访问。
func (a *App) OpenRecord(id string) error {
	rec, err := a.store.GetRecord(id)
	if err != nil {
		return err
	}
	if err := fileops.Open(rec.PathOrURL); err != nil {
		return err
	}
	// 打开成功后统计最近访问（失败不影响打开结果）
	_ = a.store.TouchRecordOpen(id)
	return nil
}

// FetchURLMeta 抓取网页标题与 favicon 地址。
func (a *App) FetchURLMeta(rawURL string) (fileops.Meta, error) {
	return fileops.FetchURLMeta(rawURL)
}

// StarRecord 切换星标状态。
// 星标为轻量状态切换，独立于通用更新且不进入撤销栈：
// 若走 TryUpdateRecord 记账，点星标会覆盖掉刚删除/新增记录的可撤销操作。
func (a *App) StarRecord(id string, star bool) error {
	return a.store.SetRecordStar(id, star)
}

// ValidateAllRecords 并发校验所有未删除记录的有效性，返回失效数量。
// 最多 8 个 worker 并行（URL 校验含网络超时，串行在万条级下不可接受）；
// 通过 validateMu 互斥，防止与启动自动校验并发重复执行。
func (a *App) ValidateAllRecords() (int, error) {
	if !a.validateMu.TryLock() {
		return 0, errors.New("校验正在进行中，请稍候")
	}
	defer a.validateMu.Unlock()

	recs, err := a.store.ListRecords(model.ListFilter{})
	if err != nil {
		return 0, err
	}

	type checkResult struct {
		id      string
		invalid bool
		was     bool
	}
	// 生产者 → worker（最多 8 并发）→ 结果收集，全程受 appCtx 取消控制
	const maxWorkers = 8
	jobs := make(chan int)
	results := make(chan checkResult, maxWorkers)
	var wg sync.WaitGroup
	for w := 0; w < maxWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				// shutdown 取消后立即停止后续校验
				select {
				case <-a.appCtx.Done():
					return
				default:
				}
				r := recs[i]
				var invalid bool
				if r.Type == model.TypeFile {
					invalid = !fileops.PathExists(r.PathOrURL)
				} else {
					invalid = !fileops.URLReachable(r.PathOrURL)
				}
				results <- checkResult{id: r.ID, invalid: invalid, was: r.Invalid}
			}
		}()
	}
	out := make([]checkResult, 0, len(recs))
	collectDone := make(chan struct{})
	go func() {
		for r := range results {
			out = append(out, r)
		}
		close(collectDone)
	}()
	go func() {
		defer close(jobs)
		for i := range recs {
			select {
			case jobs <- i:
			case <-a.appCtx.Done():
				return
			}
		}
	}()
	wg.Wait()
	close(results)
	<-collectDone

	// 结果统一落库（store 为单连接，写入本身串行）
	invalidCount := 0
	for _, r := range out {
		if r.invalid != r.was {
			_ = a.store.SetRecordInvalid(r.id, r.invalid)
		}
		if r.invalid {
			invalidCount++
		}
	}
	return invalidCount, nil
}

// EmptyTrash 清空回收站。
func (a *App) EmptyTrash() error {
	return a.store.EmptyTrash()
}

// ExportToFile 导出备份到指定路径的 JSON 文件。
func (a *App) ExportToFile(path string) error {
	b, err := a.store.Export()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化备份失败: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("写入备份文件失败: %w", err)
	}
	return nil
}

// ImportFromFile 从 JSON 文件导入备份。
func (a *App) ImportFromFile(path string) (model.ImportResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return model.ImportResult{}, fmt.Errorf("读取备份文件失败: %w", err)
	}
	var b model.Backup
	if err := json.Unmarshal(data, &b); err != nil {
		return model.ImportResult{}, fmt.Errorf("解析备份文件失败: %w", err)
	}
	return a.store.Import(&b)
}

// ChooseSnapshotPath 弹出快照保存对话框，返回 SQLite 数据库快照保存路径。
func (a *App) ChooseSnapshotPath() (string, error) {
	return runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "导出数据库快照",
		DefaultFilename: "lightmark-snapshot.db",
		Filters:         []runtime.FileFilter{{DisplayName: "SQLite 数据库 (*.db)", Pattern: "*.db"}},
	})
}

// ExportSnapshot 导出当前 SQLite 数据库的完整快照（VACUUM INTO，与在线读写一致）。
func (a *App) ExportSnapshot(path string) error {
	return a.store.SnapshotTo(path)
}

// MergeTag 将源标签的记录关联合并到目标标签并删除源标签。
func (a *App) MergeTag(srcID, dstID string) error {
	return a.store.MergeTag(srcID, dstID)
}

// SetAutoStart 开启或关闭开机自启动。
func (a *App) SetAutoStart(enable bool) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取可执行文件路径失败: %w", err)
	}
	return autostart.SetAutoStart(enable, exe)
}

// IsAutoStartEnabled 返回开机自启动是否已开启。
func (a *App) IsAutoStartEnabled() (bool, error) {
	return autostart.IsAutoStartEnabled()
}

// QuitApp 退出应用（供托盘菜单调用）。
func (a *App) QuitApp() {
	runtime.Quit(a.ctx)
}

// ChooseExportPath 弹出保存对话框，返回备份文件保存路径。
func (a *App) ChooseExportPath() (string, error) {
	return runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "导出备份",
		DefaultFilename: "lightmark-backup.json",
		Filters:         []runtime.FileFilter{{DisplayName: "JSON 文件 (*.json)", Pattern: "*.json"}},
	})
}

// ChooseImportPath 弹出打开对话框，返回备份文件路径。
func (a *App) ChooseImportPath() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "导入备份",
		Filters: []runtime.FileFilter{{DisplayName: "JSON 文件 (*.json)", Pattern: "*.json"}},
	})
}

// ChooseFilePath 弹出文件选择对话框，返回选中的本地文件路径（取消时返回空串）。
// 用于新增/编辑文件记录时选择本地路径，路径由对话框产生，前端不可手动编写。
func (a *App) ChooseFilePath() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "选择文件",
		Filters: []runtime.FileFilter{{DisplayName: "所有文件 (*.*)", Pattern: "*.*"}},
	})
}

// FetchFaviconData 下载网页 favicon 并转为 data URL（限制 64KB），便于前端直接展示。
func (a *App) FetchFaviconData(faviconURL string) (string, error) {
	return fileops.FetchFaviconData(faviconURL)
}

// ---------- 分类 ----------

// ListCategories 返回全部分类。
func (a *App) ListCategories() ([]model.Category, error) {
	return a.store.ListCategories()
}

// CreateCategory 创建分类。
func (a *App) CreateCategory(name, parentID string) (*model.Category, error) {
	return a.store.CreateCategory(name, parentID)
}

// UpdateCategory 更新分类。
func (a *App) UpdateCategory(id, name, parentID string, sort int) (*model.Category, error) {
	return a.store.UpdateCategory(id, name, parentID, sort)
}

// DeleteCategory 删除分类。
func (a *App) DeleteCategory(id string) error {
	return a.store.DeleteCategory(id)
}

// ---------- 标签 ----------

// ListTags 返回全部标签。
func (a *App) ListTags() ([]model.Tag, error) {
	return a.store.ListTags()
}

// CreateTag 创建标签。
func (a *App) CreateTag(name string) (*model.Tag, error) {
	return a.store.CreateTag(name)
}

// RenameTag 重命名标签。
func (a *App) RenameTag(id, name string) error {
	return a.store.RenameTag(id, name)
}

// DeleteTag 删除标签。
func (a *App) DeleteTag(id string) error {
	return a.store.DeleteTag(id)
}
