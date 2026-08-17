package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type mode5InsertRecord struct {
	CreatedAt string   `json:"created_at"`
	Rule      string   `json:"rule"`
	Position  string   `json:"position"`
	Count     int      `json:"count"`
	Lines     []string `json:"lines"`
}

type mode5WorkspaceFile struct {
	Index             int                 `json:"index"`
	Name              string              `json:"name"`
	Lines             []string            `json:"-"`
	OriginalLineCount int                 `json:"original_line_count"`
	CurrentLineCount  int                 `json:"current_line_count"`
	InsertedCount     int                 `json:"inserted_count"`
	InsertedLines     []string            `json:"inserted_lines"`
	Records           []mode5InsertRecord `json:"records"`
}

type mode5WorkspaceTask struct {
	TaskID    string               `json:"task_id"`
	CreatedAt string               `json:"created_at"`
	Files     []mode5WorkspaceFile `json:"files"`
}

var mode5Workspace = struct {
	sync.RWMutex
	tasks map[string]*mode5WorkspaceTask
}{tasks: map[string]*mode5WorkspaceTask{}}

func (s *AppState) mode5PrepareHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method not allowed"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, mode5MaxRequestBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "上传内容解析失败：" + err.Error()})
		return
	}
	files, err := collectMode5Files(r.MultipartForm, "insert_txt_files", "insert_archive")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if len(files) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "请上传至少一个 TXT 文件或 ZIP 压缩包"})
		return
	}

	task := &mode5WorkspaceTask{TaskID: newTaskID(), CreatedAt: time.Now().Format("2006-01-02 15:04:05")}
	for i, file := range files {
		task.Files = append(task.Files, mode5WorkspaceFile{
			Index:             i + 1,
			Name:              file.Name,
			Lines:             append([]string(nil), file.Lines...),
			OriginalLineCount: len(file.Lines),
			CurrentLineCount:  len(file.Lines),
			InsertedLines:     []string{},
			Records:           []mode5InsertRecord{},
		})
	}

	mode5Workspace.Lock()
	// 模式五工作区只用于当前处理过程，最多保留最近 20 个，避免长期占用内存。
	if len(mode5Workspace.tasks) >= 20 {
		ids := make([]string, 0, len(mode5Workspace.tasks))
		for id := range mode5Workspace.tasks {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for len(mode5Workspace.tasks) >= 20 && len(ids) > 0 {
			delete(mode5Workspace.tasks, ids[0])
			ids = ids[1:]
		}
	}
	mode5Workspace.tasks[task.TaskID] = task
	mode5Workspace.Unlock()

	s.logInfo(fmt.Sprintf("模式五已上传并解析 %d 个 TXT。", len(task.Files)))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "task": task})
}

type mode5WorkspaceInsertRequest struct {
	TaskID          string `json:"task_id"`
	Numbers         string `json:"numbers"`
	Rule            string `json:"insert_rule"`
	Position        string `json:"insert_position"`
	Suffix          string `json:"number_suffix"`
	PerFileCount    int    `json:"per_file_count"`
	FilesPerNumber  int    `json:"files_per_number"`
	TargetFileCount int    `json:"target_file_count"`
}

func (s *AppState) mode5InsertHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method not allowed"})
		return
	}
	var req mode5WorkspaceInsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "请求参数解析失败"})
		return
	}
	numbers := withSuffix(splitNonEmptyLines(req.Numbers), strings.TrimSpace(req.Suffix))
	if len(numbers) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "请填写至少一条有效号码"})
		return
	}
	if req.Position != "bottom" {
		req.Position = "top"
	}
	if req.Rule == "" {
		req.Rule = "same_all"
	}

	mode5Workspace.Lock()
	defer mode5Workspace.Unlock()
	task := mode5Workspace.tasks[req.TaskID]
	if task == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "上传记录已失效，请重新上传文件"})
		return
	}

	apply := func(index int, picked []string) {
		file := &task.Files[index]
		if req.Position == "top" {
			file.Lines = append(append([]string(nil), picked...), file.Lines...)
		} else {
			file.Lines = append(file.Lines, picked...)
		}
		file.CurrentLineCount = len(file.Lines)
		file.InsertedCount += len(picked)
		file.InsertedLines = append(file.InsertedLines, picked...)
		file.Records = append(file.Records, mode5InsertRecord{
			CreatedAt: time.Now().Format("2006-01-02 15:04:05"),
			Rule:      mode5WorkspaceRuleLabel(req.Rule),
			Position:  map[bool]string{true: "底部", false: "顶部"}[req.Position == "bottom"],
			Count:     len(picked),
			Lines:     append([]string(nil), picked...),
		})
	}

	processed := 0
	switch req.Rule {
	case "same_all":
		for i := range task.Files {
			apply(i, numbers)
			processed++
		}
	case "sequential":
		count := req.PerFileCount
		if count < 1 {
			count = 1
		}
		needed := len(task.Files) * count
		if len(numbers) < needed {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": fmt.Sprintf("号码不足：共需要 %d 条，当前只有 %d 条", needed, len(numbers))})
			return
		}
		cursor := 0
		for i := range task.Files {
			apply(i, numbers[cursor:cursor+count])
			cursor += count
			processed++
		}
	case "per_number":
		count := req.FilesPerNumber
		if count < 1 {
			count = 1
		}
		needed := len(numbers) * count
		if needed > len(task.Files) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": fmt.Sprintf("文件不足：共需要 %d 个 TXT，当前只有 %d 个", needed, len(task.Files))})
			return
		}
		cursor := 0
		for _, number := range numbers {
			for i := 0; i < count; i++ {
				apply(cursor, []string{number})
				cursor++
				processed++
			}
		}
	case "first_n":
		count := req.TargetFileCount
		if count < 1 {
			count = 1
		}
		if count > len(task.Files) {
			count = len(task.Files)
		}
		for i := 0; i < count; i++ {
			apply(i, numbers)
			processed++
		}
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "未知的插入规则"})
		return
	}

	s.logInfo(fmt.Sprintf("模式五插入完成：处理 %d 个 TXT。", processed))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": fmt.Sprintf("插入完成，共处理 %d 个 TXT。", processed), "task": task})
}

func (s *AppState) mode5DownloadHandler(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimPrefix(r.URL.Path, "/mode5/download/")
	mode5Workspace.RLock()
	defer mode5Workspace.RUnlock()
	task := mode5Workspace.tasks[taskID]
	if task == nil {
		http.Error(w, "上传记录已失效，请重新上传文件", http.StatusNotFound)
		return
	}
	filename := "模式五_批量插入结果_" + time.Now().Format("20060102_150405") + ".zip"
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	zw := zip.NewWriter(w)
	for _, file := range task.Files {
		entry, err := zw.Create(file.Name)
		if err != nil {
			_ = zw.Close()
			return
		}
		_, _ = io.WriteString(entry, strings.Join(file.Lines, "\n"))
	}
	_ = zw.Close()
}

func (s *AppState) mode5GetTaskHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method not allowed"})
		return
	}
	taskID := strings.TrimPrefix(r.URL.Path, "/mode5/task/")
	mode5Workspace.RLock()
	defer mode5Workspace.RUnlock()
	task := mode5Workspace.tasks[taskID]
	if task == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "上传记录已失效"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "task": task})
}

func (s *AppState) mode5ClearTaskHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method not allowed"})
		return
	}
	taskID := strings.TrimPrefix(r.URL.Path, "/mode5/clear/")
	mode5Workspace.Lock()
	_, existed := mode5Workspace.tasks[taskID]
	delete(mode5Workspace.tasks, taskID)
	mode5Workspace.Unlock()
	if existed {
		s.logInfo("模式五当前任务已清空。")
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *AppState) mode5PreviewHandler(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/mode5/preview/"), "/")
	if len(parts) != 2 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "预览参数错误"})
		return
	}
	index, err := strconv.Atoi(parts[1])
	if err != nil || index < 1 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "文件序号错误"})
		return
	}
	mode5Workspace.RLock()
	defer mode5Workspace.RUnlock()
	task := mode5Workspace.tasks[parts[0]]
	if task == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "上传记录已失效，请重新上传文件"})
		return
	}
	if index > len(task.Files) {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "文件不存在"})
		return
	}
	file := task.Files[index-1]
	content := strings.Join(file.Lines, "\n")
	runes := []rune(content)
	if len(runes) > 2_000_000 {
		content = string(runes[:2_000_000]) + "\n\n... 文件很大，页面仅显示前面部分。请下载 ZIP 获取完整内容。"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"filename":       file.Name,
		"line_count":     file.CurrentLineCount,
		"inserted_count": file.InsertedCount,
		"content":        content,
	})
}

func mode5WorkspaceRuleLabel(rule string) string {
	switch rule {
	case "sequential":
		return "按顺序分配"
	case "per_number":
		return "每个号码到多个 TXT"
	case "first_n":
		return "插入前 N 个 TXT"
	default:
		return "全部号码到所有 TXT"
	}
}
