package main

import (
	"archive/zip"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

func NewAppState(baseDir string) *AppState {
	// baseDir 是程序所在目录；所有数据都放在它下面，方便整体复制和备份。
	return &AppState{
		BaseDir:      baseDir,
		TaskStoreDir: filepath.Join(baseDir, taskStoreName),
		tasks:        map[string]*Task{},
		locks:        map[string]*sync.Mutex{},
	}
}

func newTaskID() string {
	// 任务 ID 使用随机值，避免多个任务目录重名。
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func (s *AppState) getTaskLock(taskID string) *sync.Mutex {
	// 每个任务一把锁：同一任务不能同时插入、撤回、删除。
	s.lockGuard.Lock()
	defer s.lockGuard.Unlock()
	lock := s.locks[taskID]
	if lock == nil {
		lock = &sync.Mutex{}
		s.locks[taskID] = lock
	}
	return lock
}

func (s *AppState) removeTaskLock(taskID string) {
	s.lockGuard.Lock()
	delete(s.locks, taskID)
	s.lockGuard.Unlock()
}

func (s *AppState) taskStorePath(taskID string) string { return filepath.Join(s.TaskStoreDir, taskID) }
func (s *AppState) taskFilesDir(taskID string) string {
	return filepath.Join(s.taskStorePath(taskID), "files")
}
func (s *AppState) taskZipDir(taskID string) string {
	return filepath.Join(s.taskStorePath(taskID), "exports")
}
func (s *AppState) taskJSONPath(taskID string) string {
	return filepath.Join(s.taskStorePath(taskID), "task.json")
}
func (s *AppState) taskUndoDir(taskID string) string {
	return filepath.Join(s.taskStorePath(taskID), "undo_last")
}
func (s *AppState) taskUndoJSONPath(taskID string) string {
	return filepath.Join(s.taskUndoDir(taskID), "task.json")
}
func (s *AppState) taskUndoFilesDir(taskID string) string {
	return filepath.Join(s.taskUndoDir(taskID), "files")
}
func (s *AppState) taskFilePath(taskID, storedName string) string {
	return filepath.Join(s.taskFilesDir(taskID), storedName)
}

func initTaskDirs(filesDir, zipDir string) error {
	// 每个任务都有 files 和 exports 两个目录：
	// files 保存拆分后的 TXT，exports 保存生成的 ZIP。
	if err := os.MkdirAll(filesDir, 0755); err != nil {
		return err
	}
	return os.MkdirAll(zipDir, 0755)
}

func makeStoredName(index int, filename string) string {
	name := filepath.Base(filename)
	name = strings.ReplaceAll(name, string(os.PathSeparator), "_")
	return fmt.Sprintf("%06d_%s", index, name)
}

func (s *AppState) addTask(task *Task) {
	s.mu.Lock()
	s.tasks[task.TaskID] = task
	s.mu.Unlock()
}

func (s *AppState) getTask(taskID string) (*Task, error) {
	s.mu.RLock()
	task := s.tasks[taskID]
	s.mu.RUnlock()
	if task == nil {
		return nil, fmt.Errorf("任务不存在或已失效，请重新拆分")
	}
	return task, nil
}

func (s *AppState) deleteTask(taskID string) {
	s.mu.Lock()
	delete(s.tasks, taskID)
	s.mu.Unlock()
}

func (s *AppState) deleteTaskCompletely(taskID string) error {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return fmt.Errorf("任务 ID 为空")
	}
	lock := s.getTaskLock(taskID)
	lock.Lock()
	defer lock.Unlock()
	if _, err := s.getTask(taskID); err != nil {
		return err
	}
	if err := os.RemoveAll(s.taskStorePath(taskID)); err != nil {
		return err
	}
	s.deleteTask(taskID)
	s.removeTaskLock(taskID)
	return nil
}

func uniqueTaskIDs(values []string) []string {
	seen := map[string]bool{}
	taskIDs := []string{}
	for _, raw := range values {
		taskID := strings.TrimSpace(raw)
		if taskID == "" || seen[taskID] {
			continue
		}
		seen[taskID] = true
		taskIDs = append(taskIDs, taskID)
	}
	return taskIDs
}

func (s *AppState) getFileDiskPath(taskID string, item FileItem) (string, error) {
	path := s.taskFilePath(taskID, item.StoredName)
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return path, nil
	}
	return "", fmt.Errorf("文件内容不存在：%s", item.Filename)
}

func countLocked(files []FileItem) int {
	count := 0
	for _, item := range files {
		if item.Locked {
			count++
		}
	}
	return count
}

func (s *AppState) listTaskSummaries(currentID string, limit int) []TaskSummary {
	s.mu.RLock()
	items := make([]TaskSummary, 0, len(s.tasks))
	for taskID, task := range s.tasks {
		totalLines := 0
		for _, item := range task.Files {
			totalLines += item.LineCount
		}
		locked := countLocked(task.Files)
		shortID := taskID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		lastModified := task.CreatedAt
		if fi, err := os.Stat(s.taskJSONPath(taskID)); err == nil {
			lastModified = fi.ModTime().Format("2006-01-02 15:04:05")
		}
		items = append(items, TaskSummary{
			TaskID:        taskID,
			ShortID:       shortID,
			CreatedAt:     task.CreatedAt,
			LastModified:  lastModified,
			FilePrefix:    task.FilePrefix,
			TotalFiles:    len(task.Files),
			LockedCount:   locked,
			UnlockedCount: len(task.Files) - locked,
			TotalLines:    totalLines,
			IsCurrent:     taskID == currentID,
			TaskMode:      task.TaskMode,
			EntryMode:     task.EntryMode,
		})
	}
	s.mu.RUnlock()
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt > items[j].CreatedAt })
	if limit > 0 && len(items) > limit {
		return items[:limit]
	}
	return items
}

func (s *AppState) saveTaskToDisk(taskID string) error {
	// 每次拆分、插入、解锁后都保存 task.json，保证重启后还能恢复任务状态。
	task, err := s.getTask(taskID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.taskStorePath(taskID), 0755); err != nil {
		return err
	}
	payload := taskPayload{
		TaskID:  taskID,
		SavedAt: time.Now().Format("2006-01-02 15:04:05"),
		Task:    task,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	// 先写 .tmp 再 Rename，避免写到一半断电导致 task.json 损坏。
	tmp := s.taskJSONPath(taskID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.taskJSONPath(taskID)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	s.logInfo("任务已保存到本地：" + s.taskJSONPath(taskID))
	return nil
}

func (s *AppState) loadTasksFromDisk() {
	// 启动时扫描 gt_tasks_go，把历史 task.json 重新加载回内存。
	entries, err := os.ReadDir(s.TaskStoreDir)
	if err != nil {
		return
	}
	loaded := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		taskID := entry.Name()
		data, err := os.ReadFile(s.taskJSONPath(taskID))
		if err != nil {
			continue
		}
		var payload taskPayload
		if err := json.Unmarshal(data, &payload); err != nil || payload.Task == nil {
			s.logWarn("恢复本地任务失败：" + s.taskJSONPath(taskID))
			continue
		}
		payload.Task.TaskID = taskID
		if payload.Task.EntryMode == "" {
			payload.Task.EntryMode = "mode1"
		}
		if payload.Task.TaskMode == "" {
			payload.Task.TaskMode = "split"
		}
		s.addTask(payload.Task)
		loaded++
	}
	if loaded > 0 {
		s.logInfo(fmt.Sprintf("已从本地恢复历史任务：%d 个。", loaded))
	}
}

func (s *AppState) buildZipFile(taskID string, selected map[int]bool) (*ZipInfo, error) {
	// selected 为 nil 表示导出全部 TXT；非 nil 表示只导出用户勾选的 TXT。
	task, err := s.getTask(taskID)
	if err != nil {
		return nil, err
	}
	var files []FileItem
	for _, item := range task.Files {
		if selected == nil || selected[item.Index] {
			files = append(files, item)
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("没有可导出的 TXT 文件")
	}
	if err := os.MkdirAll(s.taskZipDir(taskID), 0755); err != nil {
		return nil, err
	}
	locked := countLocked(task.Files)
	unlocked := len(task.Files) - locked
	var zipName string
	if selected == nil {
		zipName = fmt.Sprintf("%s结果包_共%d个_已锁定%d个_待处理%d个_%s.zip", task.FilePrefix, len(task.Files), locked, unlocked, time.Now().Format("20060102_150405"))
	} else {
		zipName = fmt.Sprintf("%s选中%d个_共%d个_已锁定%d个_待处理%d个_%s.zip", task.FilePrefix, len(files), len(task.Files), locked, unlocked, time.Now().Format("20060102_150405"))
	}
	tmp, err := os.CreateTemp(s.taskZipDir(taskID), "zip_*.tmp")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	zw := zip.NewWriter(tmp)
	for _, item := range files {
		path, err := s.getFileDiskPath(taskID, item)
		if err != nil {
			_ = zw.Close()
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
			return nil, err
		}
		if err := addFileToZip(zw, path, item.Filename); err != nil {
			_ = zw.Close()
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
			return nil, err
		}
		s.logInfo("已写入 ZIP：" + item.Filename)
	}
	if err := zw.Close(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}
	finalPath := filepath.Join(s.taskZipDir(taskID), zipName)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}
	return &ZipInfo{Name: zipName, Path: finalPath}, nil
}

func addFileToZip(zw *zip.Writer, path, name string) error {
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, src)
	return err
}

func (s *AppState) refreshExportZip(taskID string) error {
	// 每次 TXT 内容变化后重建 ZIP，确保“下载最新 ZIP”拿到的是当前状态。
	info, err := s.buildZipFile(taskID, nil)
	if err != nil {
		return err
	}
	task, err := s.getTask(taskID)
	if err == nil {
		task.ExportZip = info
	}
	s.deleteOldZips(taskID, info.Path)
	s.logInfo("已重建 ZIP 文件：" + info.Name)
	return nil
}

func (s *AppState) deleteOldZips(taskID, keepPath string) {
	entries, err := os.ReadDir(s.taskZipDir(taskID))
	if err != nil {
		return
	}
	keepAbs, _ := filepath.Abs(keepPath)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		full := filepath.Join(s.taskZipDir(taskID), entry.Name())
		fullAbs, _ := filepath.Abs(full)
		if keepAbs != "" && fullAbs == keepAbs {
			continue
		}
		_ = os.Remove(full)
	}
}

func (s *AppState) createUndoSnapshot(taskID, reason string) error {
	// 插入前复制当前 task.json 和 files 目录。
	// 如果用户插错号码，可以用“撤回上一次插入”恢复到这里。
	task, err := s.getTask(taskID)
	if err != nil {
		return err
	}
	base := s.taskStorePath(taskID)
	tmp := filepath.Join(base, "undo_last.tmp_"+newTaskID())
	if err := os.MkdirAll(tmp, 0755); err != nil {
		return err
	}
	if err := copyDir(s.taskFilesDir(taskID), filepath.Join(tmp, "files")); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	payload := map[string]any{
		"task_id":    taskID,
		"created_at": time.Now().Format("2006-01-02 15:04:05"),
		"reason":     reason,
		"task":       task,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := os.WriteFile(filepath.Join(tmp, "task.json"), data, 0644); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	_ = os.RemoveAll(s.taskUndoDir(taskID))
	if err := os.Rename(tmp, s.taskUndoDir(taskID)); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	s.logInfo("已创建撤回快照：" + reason)
	return nil
}

func (s *AppState) hasUndoSnapshot(taskID string) bool {
	if _, err := os.Stat(s.taskUndoJSONPath(taskID)); err != nil {
		return false
	}
	if info, err := os.Stat(s.taskUndoFilesDir(taskID)); err != nil || !info.IsDir() {
		return false
	}
	return true
}

func (s *AppState) restoreLastUndoSnapshot(taskID string) (*Task, error) {
	// 恢复撤回快照时，先复制到临时目录，再替换当前 files，减少恢复中断的风险。
	if !s.hasUndoSnapshot(taskID) {
		return nil, fmt.Errorf("当前任务没有可撤回的上一次插入")
	}
	data, err := os.ReadFile(s.taskUndoJSONPath(taskID))
	if err != nil {
		return nil, err
	}
	var payload struct {
		Task *Task `json:"task"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.Task == nil {
		return nil, fmt.Errorf("撤回快照内容异常，无法恢复")
	}
	tmp := filepath.Join(s.taskStorePath(taskID), "files.restore_"+newTaskID())
	if err := copyDir(s.taskUndoFilesDir(taskID), tmp); err != nil {
		return nil, err
	}
	_ = os.RemoveAll(s.taskFilesDir(taskID))
	if err := os.Rename(tmp, s.taskFilesDir(taskID)); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, err
	}
	payload.Task.TaskID = taskID
	s.addTask(payload.Task)
	s.logInfo("已恢复到上一次插入前的状态。")
	return payload.Task, nil
}

func copyDir(src, dst string) error {
	// 递归复制目录，主要用于创建撤回快照和恢复快照。
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(srcPath, dstPath); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	// 普通文件复制，调用方负责决定复制哪些目录。
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// ========== 模式四（多组号码组合）存储 ==========

func (s *AppState) mode4StoreDir() string {
	return filepath.Join(s.TaskStoreDir, "mode4_tasks")
}

// mode4TaskPath returns the path to a mode4 task JSON file
func (s *AppState) mode4TaskPath(taskID string) string {
	return filepath.Join(s.mode4StoreDir(), taskID+".json")
}

// mode4SaveTask 保存模式四任务到磁盘
func (s *AppState) mode4SaveTask(task *Mode4Task) error {
	dir := s.mode4StoreDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return err
	}
	path := s.mode4TaskPath(task.TaskID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// mode4GetTask 从磁盘读取单个模式四任务
func (s *AppState) mode4GetTask(taskID string) *Mode4Task {
	data, err := os.ReadFile(s.mode4TaskPath(taskID))
	if err != nil {
		return nil
	}
	var task Mode4Task
	if err := json.Unmarshal(data, &task); err != nil {
		return nil
	}
	return &task
}

// mode4ListTasks 列出所有模式四任务
func (s *AppState) mode4ListTasks() []*Mode4Task {
	dir := s.mode4StoreDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var tasks []*Mode4Task
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		taskID := strings.TrimSuffix(entry.Name(), ".json")
		task := s.mode4GetTask(taskID)
		if task != nil {
			tasks = append(tasks, task)
		}
	}
	// 按创建时间倒序排列
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].CreatedAt > tasks[j].CreatedAt
	})
	return tasks
}
