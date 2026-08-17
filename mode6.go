package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const mode6MaxExpandedBytes int64 = 500 * 1024 * 1024

type Mode6OutputFile struct {
	Index      int    `json:"index"`
	Filename   string `json:"filename"`
	StoredName string `json:"stored_name"`
	LineCount  int    `json:"line_count"`
}

type Mode6Record struct {
	RecordID     string            `json:"record_id"`
	CreatedAt    string            `json:"created_at"`
	Prefix       string            `json:"prefix"`
	PerFileCount int               `json:"per_file_count"`
	FileCount    int               `json:"file_count"`
	TotalCount   int               `json:"total_count"`
	StartNumber  int               `json:"start_number"`
	EndNumber    int               `json:"end_number"`
	ZipName      string            `json:"zip_name"`
	Files        []Mode6OutputFile `json:"files"`
}

type Mode6Task struct {
	TaskID      string        `json:"task_id"`
	SourceName  string        `json:"source_name"`
	CreatedAt   string        `json:"created_at"`
	TotalCount  int           `json:"total_count"`
	UsedCount   int           `json:"used_count"`
	RemainCount int           `json:"remain_count"`
	Records     []Mode6Record `json:"records"`
}

type mode6TakeRequest struct {
	TaskID       string `json:"task_id"`
	PerFileCount int    `json:"per_file_count"`
	FileCount    int    `json:"file_count"`
	Prefix       string `json:"prefix"`
}

func (s *AppState) mode6Handler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/mode6" {
		http.NotFound(w, r)
		return
	}
	s.render(w, "mode6", pageData{Title: "业务模式六 · 取数据打包系统", ActiveNav: "mode6"})
}

func (s *AppState) mode6StoreDir() string {
	return filepath.Join(s.TaskStoreDir, "mode6_tasks")
}

func (s *AppState) mode6TaskDir(taskID string) (string, error) {
	if len(taskID) < 8 || len(taskID) > 64 || filepath.Base(taskID) != taskID || strings.ContainsAny(taskID, `/\\`) {
		return "", fmt.Errorf("任务编号无效")
	}
	for _, char := range taskID {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' && char != '_' {
			return "", fmt.Errorf("任务编号无效")
		}
	}
	return filepath.Join(s.mode6StoreDir(), taskID), nil
}

func (s *AppState) mode6TaskPath(taskID string) (string, error) {
	dir, err := s.mode6TaskDir(taskID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "task.json"), nil
}

func (s *AppState) mode6SaveTask(task *Mode6Task) error {
	path, err := s.mode6TaskPath(task.TaskID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (s *AppState) mode6LoadTask(taskID string) (*Mode6Task, error) {
	path, err := s.mode6TaskPath(taskID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("任务不存在或已被清除")
		}
		return nil, err
	}
	var task Mode6Task
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, err
	}
	task.RemainCount = task.TotalCount - task.UsedCount
	return &task, nil
}

func (s *AppState) mode6UploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "只支持 POST 请求"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+10*1024*1024)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "上传内容过大或无法读取"})
		return
	}
	file, header, err := r.FormFile("total_package")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "请选择总数据包"})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxUploadBytes+1))
	if err != nil || int64(len(data)) > maxUploadBytes {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "总数据包读取失败或超过 200MB"})
		return
	}
	lines, err := mode6ExtractLines(header.Filename, data)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if len(lines) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "总数据包中没有有效号码"})
		return
	}
	taskID := newTaskID()
	dir, _ := s.mode6TaskDir(taskID)
	if err := os.MkdirAll(filepath.Join(dir, "records"), 0755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if _, _, err := writeLinesToTextFile(lines, filepath.Join(dir, "source.txt")); err != nil {
		_ = os.RemoveAll(dir)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "保存总数据失败：" + err.Error()})
		return
	}
	task := &Mode6Task{TaskID: taskID, SourceName: filepath.Base(header.Filename), CreatedAt: time.Now().Format("2006-01-02 15:04:05"), TotalCount: len(lines), RemainCount: len(lines), Records: []Mode6Record{}}
	if err := s.mode6SaveTask(task); err != nil {
		_ = os.RemoveAll(dir)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "保存任务失败：" + err.Error()})
		return
	}
	s.logInfo(fmt.Sprintf("模式六总数据包已上传：%s，共 %d 条有效数据。", task.SourceName, task.TotalCount))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "task": task})
}

func mode6ExtractLines(name string, data []byte) ([]string, error) {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == ".txt" {
		return splitNonEmptyLines(string(data)), nil
	}
	if ext != ".zip" {
		return nil, fmt.Errorf("总数据包只支持 TXT 或 ZIP 格式")
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("ZIP 压缩包无法读取")
	}
	entries := append([]*zip.File(nil), zr.File...)
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	var lines []string
	var expanded int64
	for _, entry := range entries {
		if entry.FileInfo().IsDir() || !strings.EqualFold(filepath.Ext(entry.Name), ".txt") {
			continue
		}
		remaining := mode6MaxExpandedBytes - expanded
		if remaining <= 0 || entry.UncompressedSize64 > uint64(remaining) {
			return nil, fmt.Errorf("ZIP 解压后的 TXT 总大小不能超过 500MB")
		}
		rc, openErr := entry.Open()
		if openErr != nil {
			return nil, openErr
		}
		part, readErr := io.ReadAll(io.LimitReader(rc, remaining+1))
		_ = rc.Close()
		if readErr != nil || int64(len(part)) > remaining {
			return nil, fmt.Errorf("ZIP 内文件读取失败或解压后超过 500MB")
		}
		expanded += int64(len(part))
		lines = append(lines, splitNonEmptyLines(string(part))...)
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("ZIP 中没有找到包含有效数据的 TXT 文件")
	}
	return lines, nil
}

func (s *AppState) mode6GetTaskHandler(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimPrefix(r.URL.Path, "/mode6/task/")
	task, err := s.mode6LoadTask(taskID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "task": task})
}

func (s *AppState) mode6TakeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "只支持 POST 请求"})
		return
	}
	var req mode6TakeRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024*1024)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "请求参数无法读取"})
		return
	}
	if req.PerFileCount <= 0 || req.FileCount <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "每个 TXT 数量和 TXT 文件数量都必须大于 0"})
		return
	}
	if req.PerFileCount > 10_000_000 || req.FileCount > 100_000 || req.PerFileCount > int(^uint(0)>>1)/req.FileCount {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "本次取数数量过大"})
		return
	}
	needed := req.PerFileCount * req.FileCount
	lock := s.getTaskLock("mode6_" + req.TaskID)
	lock.Lock()
	defer lock.Unlock()
	task, err := s.mode6LoadTask(req.TaskID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if needed > task.RemainCount {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": fmt.Sprintf("剩余数据不足：本次需要 %d 条，当前只剩 %d 条", needed, task.RemainCount)})
		return
	}
	dir, _ := s.mode6TaskDir(task.TaskID)
	allLines, err := readTextFileLines(filepath.Join(dir, "source.txt"))
	if err != nil || len(allLines) < task.UsedCount+needed {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "总数据文件不完整，无法继续取数"})
		return
	}
	prefix := safeFilenamePrefix(req.Prefix)
	if prefix == "" {
		prefix = "取数"
	}
	recordID := newTaskID()
	recordDir := filepath.Join(dir, "records", recordID)
	if err := os.MkdirAll(recordDir, 0755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	rollback := true
	defer func() {
		if rollback {
			_ = os.RemoveAll(recordDir)
		}
	}()
	width := len(strconv.Itoa(req.FileCount))
	if width < 2 {
		width = 2
	}
	record := Mode6Record{RecordID: recordID, CreatedAt: time.Now().Format("2006-01-02 15:04:05"), Prefix: prefix, PerFileCount: req.PerFileCount, FileCount: req.FileCount, TotalCount: needed, StartNumber: task.UsedCount + 1, EndNumber: task.UsedCount + needed}
	selected := allLines[task.UsedCount : task.UsedCount+needed]
	for i := 0; i < req.FileCount; i++ {
		filename := fmt.Sprintf("%s_%s.txt", prefix, leftPadInt(i+1, width))
		storedName := fmt.Sprintf("%06d_%s", i+1, filename)
		part := selected[i*req.PerFileCount : (i+1)*req.PerFileCount]
		count, _, writeErr := writeLinesToTextFile(part, filepath.Join(recordDir, storedName))
		if writeErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "生成 TXT 失败：" + writeErr.Error()})
			return
		}
		record.Files = append(record.Files, Mode6OutputFile{Index: i + 1, Filename: filename, StoredName: storedName, LineCount: count})
	}
	record.ZipName = fmt.Sprintf("%s_%d个TXT_每个%d条_%s.zip", prefix, req.FileCount, req.PerFileCount, time.Now().Format("20060102_150405"))
	if err := mode6BuildZip(recordDir, filepath.Join(recordDir, record.ZipName), record.Files); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "打包失败：" + err.Error()})
		return
	}
	task.UsedCount += needed
	task.RemainCount = task.TotalCount - task.UsedCount
	task.Records = append([]Mode6Record{record}, task.Records...)
	if err := s.mode6SaveTask(task); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "保存取数记录失败：" + err.Error()})
		return
	}
	rollback = false
	s.logInfo(fmt.Sprintf("模式六取数完成：生成 %d 个 TXT，每个 %d 条，共取出 %d 条。", req.FileCount, req.PerFileCount, needed))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": fmt.Sprintf("取数完成：共生成 %d 个 TXT，取出 %d 条数据", req.FileCount, needed), "task": task})
}

func mode6BuildZip(recordDir, zipPath string, files []Mode6OutputFile) error {
	f, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	for _, item := range files {
		entry, createErr := zw.Create(item.Filename)
		if createErr != nil {
			_ = zw.Close()
			_ = f.Close()
			return createErr
		}
		source, openErr := os.Open(filepath.Join(recordDir, item.StoredName))
		if openErr != nil {
			_ = zw.Close()
			_ = f.Close()
			return openErr
		}
		_, copyErr := io.Copy(entry, source)
		_ = source.Close()
		if copyErr != nil {
			_ = zw.Close()
			_ = f.Close()
			return copyErr
		}
	}
	if err := zw.Close(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func mode6FindRecord(task *Mode6Task, recordID string) (*Mode6Record, error) {
	for i := range task.Records {
		if task.Records[i].RecordID == recordID {
			return &task.Records[i], nil
		}
	}
	return nil, fmt.Errorf("取数记录不存在")
}

func (s *AppState) mode6PreviewHandler(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/mode6/preview/"), "/")
	if len(parts) != 3 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "预览地址无效"})
		return
	}
	index, err := strconv.Atoi(parts[2])
	if err != nil || index < 1 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "文件序号无效"})
		return
	}
	task, err := s.mode6LoadTask(parts[0])
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	record, err := mode6FindRecord(task, parts[1])
	if err != nil || index > len(record.Files) {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "文件不存在"})
		return
	}
	dir, _ := s.mode6TaskDir(task.TaskID)
	item := record.Files[index-1]
	content, err := readTextFile(filepath.Join(dir, "records", record.RecordID, item.StoredName), filePreviewChars)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "预览读取失败"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "filename": item.Filename, "line_count": item.LineCount, "content": content})
}

func (s *AppState) mode6DownloadHandler(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/mode6/download/"), "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	task, err := s.mode6LoadTask(parts[0])
	if err != nil {
		http.NotFound(w, r)
		return
	}
	record, err := mode6FindRecord(task, parts[1])
	if err != nil {
		http.NotFound(w, r)
		return
	}
	dir, _ := s.mode6TaskDir(task.TaskID)
	path := filepath.Join(dir, "records", record.RecordID, record.ZipName)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", urlPathEscape(record.ZipName)))
	w.Header().Set("Content-Type", "application/zip")
	http.ServeFile(w, r, path)
}

func urlPathEscape(value string) string {
	var b strings.Builder
	for _, by := range []byte(value) {
		if (by >= 'a' && by <= 'z') || (by >= 'A' && by <= 'Z') || (by >= '0' && by <= '9') || strings.ContainsRune("-_.~", rune(by)) {
			b.WriteByte(by)
		} else {
			fmt.Fprintf(&b, "%%%02X", by)
		}
	}
	return b.String()
}

func (s *AppState) mode6ClearTaskHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "只支持 POST 请求"})
		return
	}
	taskID := strings.TrimPrefix(r.URL.Path, "/mode6/clear/")
	dir, err := s.mode6TaskDir(taskID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "清除失败：" + err.Error()})
		return
	}
	s.removeTaskLock("mode6_" + taskID)
	s.logInfo("模式六当前任务已清除。")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
