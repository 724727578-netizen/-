package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func registerRoutes(mux *http.ServeMux, s *AppState) {
	mux.HandleFunc("/", s.indexHandler)
	mux.HandleFunc("/current_task", s.currentTaskHandler)
	mux.HandleFunc("/clear_logs", s.clearLogsHandler)
	mux.HandleFunc("/start_task/", s.startTaskHandler)
	mux.HandleFunc("/mode1/insert", s.mode1InsertHandler)
	mux.HandleFunc("/unified/insert", s.unifiedInsertHandler)
	mux.HandleFunc("/mode1/", s.mode1Handler)
	mux.HandleFunc("/mode1", s.mode1Handler)
	mux.HandleFunc("/mode2/check_links", s.mode2CheckLinksHandler)
	mux.HandleFunc("/mode2", s.mode2Handler)
	mux.HandleFunc("/mode4", s.mode4Handler)
	mux.HandleFunc("/mode4/create", s.mode4CreateTaskHandler)
	mux.HandleFunc("/mode4/tasks", s.mode4ListTasksHandler)
	mux.HandleFunc("/mode4/task/", s.mode4GetTaskHandler)
	mux.HandleFunc("/mode4/remark/", s.mode4UpdateRemarkHandler)
	mux.HandleFunc("/mode4/delete/", s.mode4DeleteTaskHandler)
	mux.HandleFunc("/mode4/clear_all", s.mode4ClearAllHandler)
	mux.HandleFunc("/mode4/export/", s.mode4ExportTaskHandler)
	mux.HandleFunc("/mode5/merge", s.mode5MergeHandler)
	mux.HandleFunc("/mode5/prepare", s.mode5PrepareHandler)
	mux.HandleFunc("/mode5/insert", s.mode5InsertHandler)
	mux.HandleFunc("/mode5/task/", s.mode5GetTaskHandler)
	mux.HandleFunc("/mode5/clear/", s.mode5ClearTaskHandler)
	mux.HandleFunc("/mode5/preview/", s.mode5PreviewHandler)
	mux.HandleFunc("/mode5/download/", s.mode5DownloadHandler)
	mux.HandleFunc("/mode5", s.mode5Handler)
	mux.HandleFunc("/mode6/upload", s.mode6UploadHandler)
	mux.HandleFunc("/mode6/take", s.mode6TakeHandler)
	mux.HandleFunc("/mode6/task/", s.mode6GetTaskHandler)
	mux.HandleFunc("/mode6/preview/", s.mode6PreviewHandler)
	mux.HandleFunc("/mode6/download/", s.mode6DownloadHandler)
	mux.HandleFunc("/mode6/clear/", s.mode6ClearTaskHandler)
	mux.HandleFunc("/mode6", s.mode6Handler)
	mux.HandleFunc("/mode3", s.mode3Handler)
	mux.HandleFunc("/start_task/mode3", s.startMode3TaskHandler)
	mux.HandleFunc("/mode3/insert/", s.mode3InsertHandler)
	mux.HandleFunc("/mode3/shuffle/", s.mode3ShuffleHandler)
	mux.HandleFunc("/mode3/export_zip/", s.mode3ExportZipHandler)
	mux.HandleFunc("/mode3/download_txt/", s.mode3DownloadTxtHandler)
	mux.HandleFunc("/mode3/export_selected_txt", s.mode3ExportSelectedTxtHandler)
	mux.HandleFunc("/file_preview/", s.filePreviewHandler)
	mux.HandleFunc("/undo_last_insert/", s.undoLastInsertHandler)
	mux.HandleFunc("/unlock_all/", s.unlockAllHandler)
	mux.HandleFunc("/delete_tasks", s.deleteTasksHandler)
	mux.HandleFunc("/clear_task/", s.clearTaskHandler)
	mux.HandleFunc("/delete_all/", s.deleteAllHandler)
	mux.HandleFunc("/inserted_lines/", s.insertedLinesHandler)
	mux.HandleFunc("/download_selected/", s.downloadSelectedHandler)
	mux.HandleFunc("/download/", s.downloadHandler)
	mux.HandleFunc("/shuffle_task/", s.shuffleTaskHandler)
	mux.HandleFunc("/download_txt/", s.downloadTxtHandler)
}

func redirectTo(target string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target, http.StatusSeeOther)
	}
}

func (s *AppState) render(w http.ResponseWriter, tmplName string, data pageData) {
	data.Logs = s.logsText()
	data.CurrentID = currentTaskCookie(data.CurrentID, nil)
	if data.CurrentID == "" && data.TaskID != "" {
		data.CurrentID = data.TaskID
	}
	data.Summaries = s.listTaskSummaries(data.CurrentID, 0)
	t, ok := pageTemplates[tmplName]
	if !ok {
		http.Error(w, "模板不存在："+tmplName, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func currentTaskCookie(fallback string, r *http.Request) string {
	if r == nil {
		return fallback
	}
	cookie, err := r.Cookie("current_task_id")
	if err != nil {
		return fallback
	}
	return cookie.Value
}

func (s *AppState) setCurrentTask(w http.ResponseWriter, taskID string) {
	http.SetCookie(w, &http.Cookie{Name: "current_task_id", Value: taskID, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

func (s *AppState) indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	s.render(w, "index", pageData{
		Title:     "GT TXT 本地拆分工具",
		ActiveNav: "home",
		CurrentID: currentTaskCookie("", r),
	})
}

func (s *AppState) currentTaskHandler(w http.ResponseWriter, r *http.Request) {
	taskID := currentTaskCookie("", r)
	if taskID != "" {
		if _, err := s.getTask(taskID); err == nil {
			http.Redirect(w, r, "/mode1/"+taskID, http.StatusSeeOther)
			return
		}
	}
	summaries := s.listTaskSummaries("", 1)
	if len(summaries) > 0 {
		s.setCurrentTask(w, summaries[0].TaskID)
		http.Redirect(w, r, "/mode1/"+summaries[0].TaskID, http.StatusSeeOther)
		return
	}
	s.render(w, "index", pageData{Title: "GT TXT 本地拆分工具", ActiveNav: "home", Error: "当前没有可恢复的任务，请先进入业务模式一并上传文件。"})
}

func (s *AppState) clearLogsHandler(w http.ResponseWriter, r *http.Request) {
	s.clearLogs()
	s.logInfo("日志已清空。")
	s.render(w, "index", pageData{Title: "GT TXT 本地拆分工具", ActiveNav: "home", Success: "日志已清空。", CurrentID: currentTaskCookie("", r)})
}

func (s *AppState) startTaskHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	mode := strings.TrimPrefix(r.URL.Path, "/start_task/")
	if mode != "mode1" {
		s.render(w, "index", pageData{Title: "GT TXT 本地拆分工具", ActiveNav: "home", Error: "当前只有业务模式一需要上传总数据并创建任务。"})
		return
	}
	s.clearLogs()
	s.logInfo("开始在业务模式一页面拆分总数据。")
	taskID, err := s.createTaskFromRequest(r)
	if err != nil {
		s.logErrorHint("拆分总数据", err, "请检查：是否选择了 TXT 文件、文件是否有内容、每个子 TXT 行数是否大于 0。")
		s.render(w, "mode1", pageData{Title: "业务模式一", ActiveNav: "mode1", Error: "拆分失败：" + err.Error(), CurrentID: currentTaskCookie("", r)})
		return
	}
	if err := s.saveTaskToDisk(taskID); err != nil {
		s.logErrorHint("保存任务", err, "请检查工具文件夹是否有写入权限；保存失败可能会影响重启后的任务恢复。")
	}
	s.setCurrentTask(w, taskID)
	http.Redirect(w, r, "/mode1/"+taskID, http.StatusSeeOther)
}

func (s *AppState) mode1Handler(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimPrefix(r.URL.Path, "/mode1/")
	if taskID == "/mode1" || taskID == "" {
		taskID = currentTaskCookie("", r)
		if taskID != "" {
			if _, err := s.getTask(taskID); err == nil {
				http.Redirect(w, r, "/mode1/"+taskID, http.StatusSeeOther)
				return
			}
		}
		s.render(w, "mode1", pageData{Title: "业务模式一", ActiveNav: "mode1", CurrentID: currentTaskCookie("", r)})
		return
	}
	task, err := s.getTask(taskID)
	if err != nil {
		s.render(w, "mode1", pageData{Title: "业务模式一", ActiveNav: "mode1", Error: err.Error(), CurrentID: currentTaskCookie("", r)})
		return
	}
	s.setCurrentTask(w, taskID)
	s.renderMode1(w, r, taskID, task, "", "")
}

func (s *AppState) renderMode1(w http.ResponseWriter, r *http.Request, taskID string, task *Task, success, errMsg string) {
	totalLines := 0
	for _, item := range task.Files {
		totalLines += item.LineCount
	}
	locked := countLocked(task.Files)
	scrollTarget := ""
	if taskID != "" && success != "" {
		scrollTarget = "mode1-file-list"
	} else if errMsg != "" && taskID != "" {
		if strings.Contains(errMsg, "插入") || strings.Contains(errMsg, "上传") || strings.Contains(errMsg, "数据") {
			scrollTarget = "mode1-insert-panel"
		} else {
			scrollTarget = "mode1-file-list"
		}
	}
	s.render(w, "mode1", pageData{
		Title:        "业务模式一",
		ActiveNav:    "mode1",
		CurrentID:    currentTaskCookie(taskID, r),
		Task:         task,
		TaskID:       taskID,
		Success:      success,
		Error:        errMsg,
		TotalFiles:   len(task.Files),
		Locked:       locked,
		Unlocked:     len(task.Files) - locked,
		TotalLines:   totalLines,
		HasUndo:      s.hasUndoSnapshot(taskID),
		ScrollTarget: scrollTarget,
	})
}

func (s *AppState) mode1InsertHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// 这里是“上传插入 TXT”的补充方式，表单可能包含文件，所以必须使用 multipart 解析。
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		s.logErrorHint("解析上传插入表单", err, "请确认是从页面按钮提交，不要手动改表单；如果文件过大，请先拆小。")
		s.render(w, "mode1", pageData{Title: "业务模式一", ActiveNav: "mode1", Error: "表单解析失败：" + err.Error()})
		return
	}
	s.clearLogs()
	taskID := strings.TrimSpace(r.FormValue("task_id"))

	// 同一个任务同一时间只允许一个插入操作，避免连续点击造成 TXT 内容互相覆盖。
	lock := s.getTaskLock(taskID)
	lock.Lock()
	defer lock.Unlock()
	task, err := s.getTask(taskID)
	if err == nil {
		// 插入前先创建撤回快照，用户点“撤回上一次插入”时就靠它恢复。
		err = s.createUndoSnapshot(taskID, "业务模式一插入前")
	}
	var msg string
	if err == nil {
		msg, err = s.runMode1Insert(taskID, task, r)
	}
	if err == nil {
		_ = s.saveTaskToDisk(taskID)
		s.setCurrentTask(w, taskID)
		s.renderMode1(w, r, taskID, task, msg, "")
		return
	}
	s.logErrorHint("业务模式一插入", err, "请检查：是否选择了任务、是否还有未锁定 TXT、插入内容是否足够。")
	if task != nil {
		s.renderMode1(w, r, taskID, task, "", "业务模式一失败："+err.Error())
		return
	}
	s.render(w, "mode1", pageData{Title: "业务模式一", ActiveNav: "mode1", Error: "业务模式一失败：" + err.Error()})
}

func (s *AppState) unifiedInsertHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// 统一插入现在同时支持上传 TXT 和手动粘贴，使用 multipart 解析兼容两种数据来源。
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		s.logErrorHint("解析统一插入表单", err, "请刷新页面后重新提交；如果仍失败，请把报错内容发给我。")
		s.render(w, "mode1", pageData{Title: "业务模式一", ActiveNav: "mode1", Error: "表单解析失败：" + err.Error()})
		return
	}
	s.clearLogs()
	taskID := strings.TrimSpace(r.FormValue("task_id"))
	lock := s.getTaskLock(taskID)
	lock.Lock()
	defer lock.Unlock()
	task, err := s.getTask(taskID)
	if err == nil {
		// 统一插入会批量改写 TXT，执行前必须保存快照，便于误操作后撤回。
		err = s.createUndoSnapshot(taskID, "统一插入前")
	}
	var msg string
	if err == nil {
		msg, err = s.runUnifiedInsert(taskID, task, r)
	}
	if err == nil {
		_ = s.saveTaskToDisk(taskID)
		s.setCurrentTask(w, taskID)
		s.renderMode1(w, r, taskID, task, msg, "")
		return
	}
	s.logErrorHint("统一插入", err, "请检查：策略是否选对、粘贴内容是否为空、需要处理的 TXT 数量是否超过未锁定数量。")
	if task != nil {
		s.renderMode1(w, r, taskID, task, "", "统一插入失败："+err.Error())
		return
	}
	s.render(w, "mode1", pageData{Title: "业务模式一", ActiveNav: "mode1", Error: "统一插入失败：" + err.Error()})
}

func (s *AppState) mode2Handler(w http.ResponseWriter, r *http.Request) {
	s.render(w, "mode2", pageData{Title: "业务模式二", ActiveNav: "mode2", CurrentID: currentTaskCookie("", r)})
}

func (s *AppState) filePreviewHandler(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/file_preview/"), "/")
	if len(parts) != 2 {
		http.Error(w, "参数错误", http.StatusBadRequest)
		return
	}
	taskID := parts[0]
	index, _ := strconv.Atoi(parts[1])
	task, err := s.getTask(taskID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	var item *FileItem
	for i := range task.Files {
		if task.Files[i].Index == index {
			item = &task.Files[i]
			break
		}
	}
	if item == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "文件不存在"})
		return
	}
	path, err := s.getFileDiskPath(taskID, *item)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	content, err := readTextFile(path, 2_000_000)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "filename": item.Filename, "line_count": item.LineCount, "locked": item.Locked, "content": content})
}

func (s *AppState) insertedLinesHandler(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/inserted_lines/"), "/")
	if len(parts) != 2 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "参数错误"})
		return
	}
	taskID := parts[0]
	index, _ := strconv.Atoi(parts[1])
	task, err := s.getTask(taskID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	for i := range task.Files {
		if task.Files[i].Index == index {
			lines := task.Files[i].InsertedLines
			filename := task.Files[i].Filename
			if lines == nil {
				lines = []string{}
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":             true,
				"filename":       filename,
				"inserted_lines": lines,
				"count":          len(lines),
			})
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "文件不存在"})
}

func (s *AppState) undoLastInsertHandler(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimPrefix(r.URL.Path, "/undo_last_insert/")
	lock := s.getTaskLock(taskID)
	lock.Lock()
	defer lock.Unlock()
	task, err := s.restoreLastUndoSnapshot(taskID)
	if err == nil {
		err = s.refreshExportZip(taskID)
	}
	if err == nil {
		err = s.saveTaskToDisk(taskID)
	}
	if err == nil {
		_ = os.RemoveAll(s.taskUndoDir(taskID))
		s.setCurrentTask(w, taskID)
		s.renderMode1(w, r, taskID, task, "已撤回上一次操作，TXT 文件包已恢复到操作前状态。", "")
		return
	}
	s.logError("撤回上一次插入失败：" + err.Error())
	if task, getErr := s.getTask(taskID); getErr == nil {
		s.renderMode1(w, r, taskID, task, "", "撤回上一次插入失败："+err.Error())
		return
	}
	s.render(w, "index", pageData{Title: "GT TXT 本地拆分工具", ActiveNav: "home", Error: "撤回上一次插入失败：" + err.Error()})
}

func (s *AppState) unlockAllHandler(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimPrefix(r.URL.Path, "/unlock_all/")
	lock := s.getTaskLock(taskID)
	lock.Lock()
	defer lock.Unlock()
	task, err := s.getTask(taskID)
	if err == nil {
		for i := range task.Files {
			task.Files[i].Locked = false
			task.Files[i].Inserted = false
			task.Files[i].InsertedCount = 0
			task.Files[i].InsertedLines = nil
			task.Files[i].BusinessMode = ""
		}
		err = s.refreshExportZip(taskID)
	}
	if err == nil {
		_ = s.saveTaskToDisk(taskID)
		s.setCurrentTask(w, taskID)
	}
	http.Redirect(w, r, "/mode1/"+taskID, http.StatusSeeOther)
}

func (s *AppState) clearTaskHandler(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimPrefix(r.URL.Path, "/clear_task/")
	lock := s.getTaskLock(taskID)
	lock.Lock()
	defer lock.Unlock()
	s.deleteTask(taskID)
	_ = os.RemoveAll(s.taskStorePath(taskID))
	s.removeTaskLock(taskID)
	http.SetCookie(w, &http.Cookie{Name: "current_task_id", Value: "", Path: "/", MaxAge: -1})
	s.render(w, "index", pageData{Title: "GT TXT 本地拆分工具", ActiveNav: "home", Success: "已一键清空当前任务全部信息，请重新上传主包。"})
}

func (s *AppState) deleteTasksHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.render(w, "index", pageData{Title: "GT TXT 本地拆分工具", ActiveNav: "home", Error: "解析删除任务表单失败：" + err.Error(), CurrentID: currentTaskCookie("", r)})
		return
	}
	taskIDs := uniqueTaskIDs(r.Form["task_ids"])
	if len(taskIDs) == 0 {
		s.render(w, "index", pageData{Title: "GT TXT 本地拆分工具", ActiveNav: "home", Error: "请至少勾选一个历史任务再删除。", CurrentID: currentTaskCookie("", r)})
		return
	}

	currentID := currentTaskCookie("", r)
	deletedCurrent := false
	deleted := 0
	for _, taskID := range taskIDs {
		if err := s.deleteTaskCompletely(taskID); err != nil {
			s.logWarn("删除历史任务失败：" + taskID + "，原因：" + err.Error())
			continue
		}
		deleted++
		if taskID == currentID {
			deletedCurrent = true
		}
	}
	if deletedCurrent {
		currentID = ""
		http.SetCookie(w, &http.Cookie{Name: "current_task_id", Value: "", Path: "/", MaxAge: -1})
	}
	if deleted == 0 {
		s.render(w, "index", pageData{Title: "GT TXT 本地拆分工具", ActiveNav: "home", Error: "删除失败：选中的历史任务不存在或已经被删除。", CurrentID: currentID})
		return
	}
	s.logInfo(fmt.Sprintf("已删除历史任务：%d 个。", deleted))
	s.render(w, "index", pageData{Title: "GT TXT 本地拆分工具", ActiveNav: "home", Success: fmt.Sprintf("已删除选中的 %d 个历史任务。", deleted), CurrentID: currentID})
}

func (s *AppState) deleteAllHandler(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimPrefix(r.URL.Path, "/delete_all/")
	lock := s.getTaskLock(taskID)
	lock.Lock()
	defer lock.Unlock()
	task, err := s.getTask(taskID)
	if err == nil {
		task.Files = nil
		task.ExportZip = nil
		_ = os.RemoveAll(s.taskFilesDir(taskID))
		_ = os.MkdirAll(s.taskFilesDir(taskID), 0755)
		_ = os.RemoveAll(s.taskUndoDir(taskID))
		_ = s.saveTaskToDisk(taskID)
		s.renderMode1(w, r, taskID, task, "已一键删除当前任务下所有 TXT 文件包。", "")
		return
	}
	s.render(w, "index", pageData{Title: "GT TXT 本地拆分工具", ActiveNav: "home", Error: "一键删除失败：" + err.Error()})
}

func (s *AppState) downloadHandler(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimPrefix(r.URL.Path, "/download/")
	lock := s.getTaskLock(taskID)
	lock.Lock()
	task, err := s.getTask(taskID)
	if err == nil && (task.ExportZip == nil || task.ExportZip.Path == "" || !fileExists(task.ExportZip.Path)) {
		err = s.refreshExportZip(taskID)
		_ = s.saveTaskToDisk(taskID)
	}
	var info *ZipInfo
	if err == nil {
		info = task.ExportZip
	}
	lock.Unlock()
	if err != nil || info == nil || !fileExists(info.Path) {
		http.Error(w, "下载文件不存在或尚未生成", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(info.Name)))
	http.ServeFile(w, r, info.Path)
}

func (s *AppState) downloadSelectedHandler(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimPrefix(r.URL.Path, "/download_selected/")
	// 导出选中只是复选框普通表单，不包含文件上传。
	if err := r.ParseForm(); err != nil {
		s.logErrorHint("解析导出选中表单", err, "请返回页面重新勾选 TXT 后再导出。")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	selected := selectedIndexesFromValues(r.Form)
	if len(selected) == 0 {
		s.logWarn("导出选中被取消：没有勾选任何 TXT 文件。")
		http.Error(w, "请至少勾选一个 TXT 文件再导出", http.StatusBadRequest)
		return
	}
	lock := s.getTaskLock(taskID)
	lock.Lock()
	info, err := s.buildZipFile(taskID, selected)
	lock.Unlock()
	if err != nil || info == nil || !fileExists(info.Path) {
		if err == nil {
			err = fmt.Errorf("没有生成可下载的 ZIP 文件")
		}
		s.logErrorHint("导出选中 ZIP", err, "请确认选中的 TXT 还存在；如果任务异常，可重新下载最新 ZIP 或重新拆分。")
		http.Error(w, "选中导出失败："+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(info.Name)))
	http.ServeFile(w, r, info.Path)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// ========== 业务模式三 处理器 ==========

// mode3Handler 渲染业务模式三页面
func (s *AppState) mode3Handler(w http.ResponseWriter, r *http.Request) {
	ck, _ := r.Cookie("current_task_id")
	taskID := ""
	var task *Task
	if ck != nil {
		taskID = ck.Value
		if t, e := s.getTask(taskID); e == nil && t.EntryMode == "mode3" {
			task = t
		} else {
			taskID = ""
		}
	}
	s.renderMode3(w, r, taskID, task, "", "")
}

// renderMode3 渲染模式三页面
func (s *AppState) renderMode3(w http.ResponseWriter, r *http.Request, taskID string, task *Task, success, errMsg string) {
	totalFiles, totalLines, lockedCount := 0, 0, 0
	hasUndo := false
	if task != nil {
		totalFiles = len(task.Files)
		for _, f := range task.Files {
			totalLines += f.LineCount
			if f.Locked {
				lockedCount++
			}
		}
		hasUndo = s.hasUndoSnapshot(taskID)
	}
	scrollTarget := ""
	if taskID != "" && success != "" {
		scrollTarget = "mode3-file-list"
	} else if errMsg != "" && taskID != "" {
		if strings.Contains(errMsg, "插入") || strings.Contains(errMsg, "数据") {
			scrollTarget = "mode3-insert-panel"
		} else if strings.Contains(errMsg, "任务") && strings.Contains(errMsg, "创建") {
			scrollTarget = "mode3-create-task"
		} else {
			scrollTarget = "mode3-file-list"
		}
	}

	data := pageData{
		Title:        "业务模式三 — TXT 数据加工",
		ActiveNav:    "mode3",
		TaskID:       taskID,
		Task:         task,
		TotalFiles:   totalFiles,
		TotalLines:   totalLines,
		Locked:       lockedCount,
		Unlocked:     totalFiles - lockedCount,
		HasUndo:      hasUndo,
		Success:      success,
		Error:        errMsg,
		ScrollTarget: scrollTarget,
	}
	s.render(w, "mode3", data)
}

// startMode3TaskHandler 创建模式三任务
// 复用 createTaskFromRequest，然后修改 EntryMode 为 mode3
func (s *AppState) startMode3TaskHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	taskID, err := s.createTaskFromRequest(r)
	if err != nil {
		s.renderMode3(w, r, "", nil, "", err.Error())
		return
	}
	// 修改 EntryMode 为 mode3
	if task, e := s.getTask(taskID); e == nil {
		task.EntryMode = "mode3"
		s.saveTaskToDisk(taskID)
		s.setCurrentTask(w, taskID)
		s.renderMode3(w, r, taskID, task, "任务创建成功！", "")
		return
	}
	s.renderMode3(w, r, "", nil, "", "")
}

// mode3InsertHandler 模式三：向选中文件插入号码
// POST /mode3/insert/{taskID}
// Form: selected[]=1&selected[]=5, insert_txt/paste_text, insert_position, append_suffix, insert_mode (same/sequence), per_txt_count
func (s *AppState) mode3InsertHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		s.logErrorHint("解析模式三插入表单", err, "请刷新页面后重新提交。")
		s.renderMode3(w, r, "", nil, "", "表单解析失败："+err.Error())
		return
	}
	s.clearLogs()

	taskID := strings.TrimPrefix(r.URL.Path, "/mode3/insert/")
	lock := s.getTaskLock(taskID)
	lock.Lock()
	defer lock.Unlock()

	task, err := s.getTask(taskID)
	if err != nil {
		s.renderMode3(w, r, "", nil, "", "任务不存在："+err.Error())
		return
	}

	// 解析选中的文件索引
	selectedRaw := r.Form["selected"]
	if len(selectedRaw) == 0 {
		s.renderMode3(w, r, taskID, task, "", "请至少选择一个 TXT 文件")
		return
	}
	selected := map[int]bool{}
	for _, raw := range selectedRaw {
		idx, err := strconv.Atoi(strings.TrimSpace(raw))
		if err == nil && idx > 0 {
			selected[idx] = true
		}
	}
	if len(selected) == 0 {
		s.renderMode3(w, r, taskID, task, "", "请选择有效的文件索引")
		return
	}

	// 获取插入数据
	lines, sourceName, err := linesFromUploadOrPaste(r, "insert_txt", "paste_text")
	if err != nil {
		s.renderMode3(w, r, taskID, task, "", "获取插入数据失败："+err.Error())
		return
	}
	lines = withSuffix(lines, strings.TrimSpace(r.FormValue("append_suffix")))
	if len(lines) == 0 {
		s.renderMode3(w, r, taskID, task, "", "插入数据没有有效内容")
		return
	}

	insertPosition := r.FormValue("insert_position")
	if insertPosition == "" {
		insertPosition = "top"
	}
	insertMode := r.FormValue("insert_mode")
	if insertMode == "" {
		insertMode = "same"
	}
	perTxtCount, _ := parsePositiveInt(r.FormValue("per_txt_count"), 1, "每个TXT插入条数")

	s.logInfo("模式三数据来源：" + sourceName)

	// 插入前创建撤回快照
	if err := s.createUndoSnapshot(taskID, "模式三插入前"); err != nil {
		s.renderMode3(w, r, taskID, task, "", "创建撤回快照失败："+err.Error())
		return
	}

	// 收集选中且未锁定的文件
	var targets []int
	for i := range task.Files {
		if selected[task.Files[i].Index] && !task.Files[i].Locked {
			targets = append(targets, i)
		}
	}
	if len(targets) == 0 {
		s.renderMode3(w, r, taskID, task, "", "选中的文件均已锁定，无法重复插入")
		return
	}

	processed := 0
	if insertMode == "same" {
		// 相同内容模式：每个选中文件插入全部内容
		for _, idx := range targets {
			if err := s.rewriteItemWithInsert(taskID, &task.Files[idx], lines, insertPosition, "模式三-全部插入", len(lines)); err != nil {
				s.logError(fmt.Sprintf("插入文件 %s 失败：%v", task.Files[idx].Filename, err))
				continue
			}
			s.logInfo(fmt.Sprintf("%s 已完成插入并锁定，本次插入 %d 条。", task.Files[idx].Filename, len(lines)))
			processed++
		}
	} else {
		// 顺序分配模式：按顺序每个TXT分配 per_txt_count 条
		cursor := 0
		for _, idx := range targets {
			if cursor >= len(lines) {
				s.logWarn(fmt.Sprintf("插入数据已用完，跳过后续文件"))
				break
			}
			end := cursor + perTxtCount
			if end > len(lines) {
				end = len(lines)
			}
			picked := lines[cursor:end]
			cursor = end
			if err := s.rewriteItemWithInsert(taskID, &task.Files[idx], picked, insertPosition, "模式三-顺序分配", len(picked)); err != nil {
				s.logError(fmt.Sprintf("插入文件 %s 失败：%v", task.Files[idx].Filename, err))
				continue
			}
			s.logInfo(fmt.Sprintf("%s 已完成顺序插入并锁定，本次插入 %d 条。", task.Files[idx].Filename, len(picked)))
			processed++
		}
	}

	if processed == 0 {
		s.renderMode3(w, r, taskID, task, "", "没有文件被成功处理，请检查选中文件状态和插入数据")
		return
	}

	if err := s.refreshExportZip(taskID); err != nil {
		s.logError("刷新 ZIP 失败：" + err.Error())
	}
	if err := s.saveTaskToDisk(taskID); err != nil {
		s.logError("保存任务失败：" + err.Error())
	}
	s.setCurrentTask(w, taskID)
	s.renderMode3(w, r, taskID, task, fmt.Sprintf("模式三插入完成，共处理 %d 个文件。", processed), "")
}

// mode3ShuffleHandler 模式三：随机打乱选中文件
// POST /mode3/shuffle/{taskID}
// Form: selected[]=1&selected[]=5
func (s *AppState) mode3ShuffleHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderMode3(w, r, "", nil, "", "表单解析失败："+err.Error())
		return
	}

	taskID := strings.TrimPrefix(r.URL.Path, "/mode3/shuffle/")
	lock := s.getTaskLock(taskID)
	lock.Lock()
	defer lock.Unlock()

	task, err := s.getTask(taskID)
	if err != nil {
		s.renderMode3(w, r, "", nil, "", "任务不存在："+err.Error())
		return
	}

	// 解析选中文件索引
	selectedRaw := r.Form["selected"]
	selected := map[int]bool{}
	for _, raw := range selectedRaw {
		idx, err := strconv.Atoi(strings.TrimSpace(raw))
		if err == nil && idx > 0 {
			selected[idx] = true
		}
	}
	if len(selected) == 0 {
		s.renderMode3(w, r, taskID, task, "", "请至少选择一个 TXT 文件")
		return
	}

	// 打乱前创建撤回快照
	if err := s.createUndoSnapshot(taskID, "模式三打乱前"); err != nil {
		s.renderMode3(w, r, taskID, task, "", "创建撤回快照失败："+err.Error())
		return
	}

	shuffled := 0
	for i := range task.Files {
		if selected[task.Files[i].Index] {
			if err := s.shuffleFile(taskID, task.Files[i].Index); err != nil {
				s.logWarn(fmt.Sprintf("打乱文件 %s 失败：%v", task.Files[i].Filename, err))
				continue
			}
			shuffled++
		}
	}

	if shuffled == 0 {
		s.renderMode3(w, r, taskID, task, "", "没有文件被成功打乱")
		return
	}

	if err := s.refreshExportZip(taskID); err != nil {
		s.logError("刷新 ZIP 失败：" + err.Error())
	}
	if err := s.saveTaskToDisk(taskID); err != nil {
		s.logError("保存任务失败：" + err.Error())
	}
	s.setCurrentTask(w, taskID)
	s.renderMode3(w, r, taskID, task, fmt.Sprintf("模式三打乱完成，共打乱 %d 个文件。", shuffled), "")
}

// mode3ExportZipHandler 模式三：导出选中文件为 ZIP
// POST /mode3/export_zip/{taskID}
// Form: selected[]=1&selected[]=5
func (s *AppState) mode3ExportZipHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "表单解析失败："+err.Error(), http.StatusBadRequest)
		return
	}

	taskID := strings.TrimPrefix(r.URL.Path, "/mode3/export_zip/")

	// 解析选中文件索引
	selectedRaw := r.Form["selected"]
	selected := map[int]bool{}
	for _, raw := range selectedRaw {
		idx, err := strconv.Atoi(strings.TrimSpace(raw))
		if err == nil && idx > 0 {
			selected[idx] = true
		}
	}
	if len(selected) == 0 {
		http.Error(w, "请至少选择一个 TXT 文件", http.StatusBadRequest)
		return
	}

	lock := s.getTaskLock(taskID)
	lock.Lock()
	info, err := s.buildZipFile(taskID, selected)
	lock.Unlock()

	if err != nil || info == nil || !fileExists(info.Path) {
		if err == nil {
			err = fmt.Errorf("没有生成可下载的 ZIP 文件")
		}
		s.logErrorHint("模式三导出 ZIP", err, "请确认选中的 TXT 还存在。")
		http.Error(w, "导出失败："+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(info.Name)))
	http.ServeFile(w, r, info.Path)
}

// mode3DownloadTxtHandler 模式三：下载单个 TXT 文件
// GET /mode3/download_txt/{taskID}/{index}
func (s *AppState) mode3DownloadTxtHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/mode3/download_txt/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		http.Error(w, "参数错误", http.StatusBadRequest)
		return
	}
	taskID := parts[0]
	index, _ := strconv.Atoi(parts[1])

	task, err := s.getTask(taskID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	var item *FileItem
	for i := range task.Files {
		if task.Files[i].Index == index {
			item = &task.Files[i]
			break
		}
	}
	if item == nil {
		http.Error(w, "文件不存在", http.StatusNotFound)
		return
	}
	diskPath, err := s.getFileDiskPath(taskID, *item)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", item.Filename))
	http.ServeFile(w, r, diskPath)
}

// ========== 业务模式四 处理器 ==========

// mode4Handler 渲染模式四页面
func (s *AppState) mode4Handler(w http.ResponseWriter, r *http.Request) {
	taskList := s.mode4ListTasks()
	taskListJSON := "[]"
	if len(taskList) > 0 {
		b, err := json.Marshal(taskList)
		if err == nil {
			taskListJSON = string(b)
		}
	}

	taskDataJSON := ""
	taskID := r.URL.Query().Get("task_id")
	if taskID != "" {
		if t := s.mode4GetTask(taskID); t != nil {
			b, err := json.Marshal(t)
			if err == nil {
				taskDataJSON = string(b)
			}
		}
	}

	s.render(w, "mode4", pageData{
		Title:         "业务模式四 — 多组号码组合",
		ActiveNav:     "mode4",
		Mode4TaskList: taskListJSON,
		Mode4TaskData: taskDataJSON,
	})
}

// mode4CreateTaskHandler 创建模式四任务
// POST /mode4/create
// Body: {"remark":"...","groups":[{"group_name":"...","mode":"align|fixed","raw_text":"..."}]}
func (s *AppState) mode4CreateTaskHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method not allowed"})
		return
	}

	var req struct {
		Remark         string       `json:"remark"`
		Groups         []Mode4Group `json:"groups"`
		ResultGroupCnt int          `json:"result_group_count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "请求参数解析失败：" + err.Error()})
		return
	}

	if len(req.Groups) < 2 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "至少需要两组号码"})
		return
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	taskID := newTaskID()

	// 备注为空时自动生成名称：3位ID_MM-DD
	remark := req.Remark
	if strings.TrimSpace(remark) == "" {
		today := time.Now().Format("01-02")
		maxID := 0
		for _, t := range s.mode4ListTasks() {
			if strings.HasSuffix(t.Remark, "_"+today) {
				parts := strings.SplitN(t.Remark, "_", 2)
				if len(parts) == 2 {
					if id, err := strconv.Atoi(parts[0]); err == nil && id > maxID {
						maxID = id
					}
				}
			}
		}
		remark = fmt.Sprintf("%03d_%s", maxID+1, today)
	}

	// 清洗并构建 groups
	var groups []Mode4Group
	for i, g := range req.Groups {
		lines := strings.Split(g.RawText, "\n")
		var cleanLines []string
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" {
				cleanLines = append(cleanLines, line)
			}
		}
		mode := g.Mode
		if mode == "" {
			mode = "align"
		}
		groupName := g.GroupName
		if groupName == "" {
			groupName = fmt.Sprintf("第%d组", i+1)
		}
		insCnt := g.InsertGroupCount
		if insCnt < 1 {
			insCnt = 1
		}
		bnc := g.BatchNumberCount
		if bnc < 1 {
			bnc = 1
		}
		bgc := g.BatchGroupCount
		if bgc < 1 {
			bgc = 1
		}
		ric := g.RandInsertCount
		if ric < 1 {
			ric = 1
		}
		groups = append(groups, Mode4Group{
			GroupIndex:       i + 1,
			GroupName:        groupName,
			Mode:             mode,
			RawText:          g.RawText,
			Lines:            cleanLines,
			LineCount:        len(cleanLines),
			InsertGroupCount: insCnt,
			BatchNumberCount: bnc,
			BatchGroupCount:  bgc,
			RandInsertCount:  ric,
			AllowRepeat:      g.AllowRepeat,
		})
	}

	// 确定生成小组数量
	resultGroups := req.ResultGroupCnt
	if resultGroups < 1 {
		resultGroups = groups[0].LineCount
	}
	if resultGroups < 1 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "无法确定生成小组数量，请检查第一组号码是否有有效数据"})
		return
	}

	// 为所有组（索引>=0）预计算每行的插入位置
	groupRowMap := make(map[int]map[int][]string)
	// 统计信息
	type groupUsage struct {
		Used   int // 实际使用的号码数
		Unused int // 未使用的号码数
	}
	usageMap := make(map[int]*groupUsage)

	for gi := 0; gi < len(groups); gi++ {
		g := groups[gi]
		rowMap := make(map[int][]string)
		gu := &groupUsage{}
		useSeed := int64(gi)

		switch g.Mode {
		case "fixed":
			// 固定号码插入全部小组
			if len(g.Lines) > 0 {
				for rIdx := 0; rIdx < resultGroups; rIdx++ {
					rowMap[rIdx] = []string{g.Lines[0]}
				}
				gu.Used = 1
			}

		case "repeat":
			// 每个号码插入 insert_group_count 个小组
			cnt := g.InsertGroupCount
			if cnt < 1 {
				cnt = 1
			}
			for li, num := range g.Lines {
				startRow := li * cnt
				endRow := startRow + cnt
				if startRow >= resultGroups {
					gu.Unused = len(g.Lines) - li
					break
				}
				if endRow > resultGroups {
					endRow = resultGroups
				}
				for rIdx := startRow; rIdx < endRow; rIdx++ {
					rowMap[rIdx] = append(rowMap[rIdx], num)
				}
				gu.Used++
			}

		case "batch":
			// 每批 batch_number_count 个号码插入 batch_group_count 个小组
			bnc := g.BatchNumberCount
			if bnc < 1 {
				bnc = 1
			}
			bgc := g.BatchGroupCount
			if bgc < 1 {
				bgc = 1
			}
			batchIdx := 0
			li := 0
			for li < len(g.Lines) {
				startRow := batchIdx * bgc
				endRow := startRow + bgc
				if startRow >= resultGroups {
					gu.Unused = len(g.Lines) - li
					break
				}
				if endRow > resultGroups {
					endRow = resultGroups
				}
				// 该批的号码
				batchEnd := li + bnc
				if batchEnd > len(g.Lines) {
					batchEnd = len(g.Lines)
				}
				batchNums := g.Lines[li:batchEnd]
				for rIdx := startRow; rIdx < endRow; rIdx++ {
					rowMap[rIdx] = append(rowMap[rIdx], batchNums...)
				}
				gu.Used += len(batchNums)
				li = batchEnd
				batchIdx++
			}

		case "random":
			// 随机插入
			ric := g.RandInsertCount
			if ric < 1 {
				ric = 1
			}
			if len(g.Lines) == 0 {
				break
			}
			rng := rand.New(rand.NewSource(useSeed + time.Now().UnixNano()))
			usedPool := make(map[int]bool)

			for rIdx := 0; rIdx < resultGroups; rIdx++ {
				var picked []string
				if g.AllowRepeat {
					for k := 0; k < ric; k++ {
						idx := rng.Intn(len(g.Lines))
						picked = append(picked, g.Lines[idx])
					}
				} else {
					pool := make([]int, 0, len(g.Lines))
					for idx := range g.Lines {
						if !usedPool[idx] {
							pool = append(pool, idx)
						}
					}
					rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
					need := ric
					if len(pool) < need {
						need = len(pool)
					}
					for k := 0; k < need; k++ {
						idx := pool[k]
						picked = append(picked, g.Lines[idx])
						usedPool[idx] = true
					}
				}
				if len(picked) > 0 {
					rowMap[rIdx] = picked
					gu.Used += len(picked)
				}
			}
			if !g.AllowRepeat {
				gu.Unused = len(g.Lines) - len(usedPool)
			}
		default: // align
			// 按行对齐
			for rIdx := 0; rIdx < resultGroups; rIdx++ {
				if rIdx < len(g.Lines) {
					rowMap[rIdx] = []string{g.Lines[rIdx]}
					gu.Used++
				}
			}
			if len(g.Lines) > resultGroups {
				gu.Unused = len(g.Lines) - resultGroups
			}
		}

		groupRowMap[gi] = rowMap
		usageMap[gi] = gu
	}

	// 生成组合结果
	var resultLines []string
	for rIdx := 0; rIdx < resultGroups; rIdx++ {
		resultLines = append(resultLines, fmt.Sprintf("第%d组:", rIdx+1))

		// 按号码组顺序依次插入
		for gi := 0; gi < len(groups); gi++ {
			if nums, ok := groupRowMap[gi][rIdx]; ok {
				for _, num := range nums {
					resultLines = append(resultLines, num)
				}
			}
		}

		resultLines = append(resultLines, "") // 空行分隔
	}

	resultText := strings.Join(resultLines, "\n")

	task := &Mode4Task{
		TaskID:       taskID,
		Remark:       remark,
		CreatedAt:    now,
		UpdatedAt:    now,
		Groups:       groups,
		ResultText:   resultText,
		GroupCount:   len(groups),
		ResultGroups: resultGroups,
		Exported:     false,
		ExportCount:  0,
	}

	if err := s.mode4SaveTask(task); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "保存任务失败：" + err.Error()})
		return
	}

	// 构建统计信息
	type statItem struct {
		Name      string `json:"name"`
		LineCount int    `json:"line_count"`
		Mode      string `json:"mode"`
		ModeLabel string `json:"mode_label"`
		Used      int    `json:"used"`
		Unused    int    `json:"unused"`
	}
	var stats []statItem
	var notices []string
	for gi, g := range groups {
		ml := modeLabel(g.Mode)
		si := statItem{
			Name:      g.GroupName,
			LineCount: g.LineCount,
			Mode:      g.Mode,
			ModeLabel: ml,
		}
		if u, ok := usageMap[gi]; ok {
			si.Used = u.Used
			si.Unused = u.Unused
		} else {
			si.Used = g.LineCount
		}
		stats = append(stats, si)

		if si.Unused > 0 {
			notices = append(notices, fmt.Sprintf("%s有 %d 条号码未参与组合。", g.GroupName, si.Unused))
		}
	}
	// align 组数量不匹配提示（相对第一组）
	if len(groups) > 0 {
		firstCount := groups[0].LineCount
		for gi := 1; gi < len(groups); gi++ {
			g := groups[gi]
			if g.Mode != "align" {
				continue
			}
			if g.LineCount < firstCount {
				notices = append(notices, fmt.Sprintf("%s只有 %d 条，少于第一组 %d 条，后 %d 个小组将只包含第一组号码。", g.GroupName, g.LineCount, firstCount, firstCount-g.LineCount))
			} else if g.LineCount > firstCount {
				notices = append(notices, fmt.Sprintf("%s有 %d 条，多于第一组 %d 条，多出的 %d 条不会参与组合。", g.GroupName, g.LineCount, firstCount, g.LineCount-firstCount))
			}
		}
	}

	s.logInfo(fmt.Sprintf("模式四任务 [%s] 创建成功：%d 组号码, 生成 %d 组结果", taskID, len(groups), resultGroups))
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"task_id": taskID,
		"groups":  resultGroups,
		"stats":   stats,
		"notices": notices,
	})
}

// modeLabel 返回模式的中文名称
func modeLabel(m string) string {
	switch m {
	case "fixed":
		return "固定号码插入全部小组"
	case "repeat":
		return "每个号码插入多个小组"
	case "batch":
		return "多个号码分批插入多个小组"
	case "random":
		return "随机插入"
	default:
		return "按行对齐"
	}
}

// mode4ListTasksHandler 获取模式四任务列表
// GET /mode4/tasks
func (s *AppState) mode4ListTasksHandler(w http.ResponseWriter, r *http.Request) {
	tasks := s.mode4ListTasks()
	type taskSummary struct {
		TaskID         string `json:"task_id"`
		Remark         string `json:"remark"`
		CreatedAt      string `json:"created_at"`
		GroupCount     int    `json:"group_count"`
		ResultGroups   int    `json:"result_groups"`
		Exported       bool   `json:"exported"`
		ExportCount    int    `json:"export_count"`
		LastExportedAt string `json:"last_exported_at,omitempty"`
	}
	var summaries []taskSummary
	for _, t := range tasks {
		summaries = append(summaries, taskSummary{
			TaskID:         t.TaskID,
			Remark:         t.Remark,
			CreatedAt:      t.CreatedAt,
			GroupCount:     t.GroupCount,
			ResultGroups:   t.ResultGroups,
			Exported:       t.Exported,
			ExportCount:    t.ExportCount,
			LastExportedAt: t.LastExportedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": summaries})
}

// mode4GetTaskHandler 获取单个模式四任务
// GET /mode4/task/{task_id}
func (s *AppState) mode4GetTaskHandler(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimPrefix(r.URL.Path, "/mode4/task/")
	if taskID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "任务 ID 为空"})
		return
	}
	task := s.mode4GetTask(taskID)
	if task == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "任务不存在"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"task": task})
}

// mode4UpdateRemarkHandler 更新模式四任务备注
// POST /mode4/remark/{task_id}
// Body: {"remark":"..."}
func (s *AppState) mode4UpdateRemarkHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method not allowed"})
		return
	}
	taskID := strings.TrimPrefix(r.URL.Path, "/mode4/remark/")
	if taskID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "任务 ID 为空"})
		return
	}

	var req struct {
		Remark string `json:"remark"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "请求参数解析失败：" + err.Error()})
		return
	}

	task := s.mode4GetTask(taskID)
	if task == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "任务不存在"})
		return
	}

	task.Remark = req.Remark
	task.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")

	if err := s.mode4SaveTask(task); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "保存失败：" + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// mode4DeleteTaskHandler 删除模式四任务
// POST /mode4/delete/{task_id}
func (s *AppState) mode4DeleteTaskHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method not allowed"})
		return
	}
	taskID := strings.TrimPrefix(r.URL.Path, "/mode4/delete/")
	if taskID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "任务 ID 为空"})
		return
	}
	if err := os.Remove(s.mode4TaskPath(taskID)); err != nil && !os.IsNotExist(err) {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "删除失败：" + err.Error()})
		return
	}
	s.logInfo(fmt.Sprintf("模式四任务 [%s] 已删除", taskID))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// mode4ClearAllHandler 清空模式四全部历史任务
// POST /mode4/clear_all
func (s *AppState) mode4ClearAllHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method not allowed"})
		return
	}
	tasks := s.mode4ListTasks()
	deletedCount := 0
	for _, t := range tasks {
		if err := os.Remove(s.mode4TaskPath(t.TaskID)); err == nil {
			deletedCount++
		}
	}
	s.logInfo(fmt.Sprintf("模式四清空历史记录：共删除 %d 个任务", deletedCount))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": deletedCount})
}

// mode4ExportTaskHandler 导出模式四任务结果为 TXT 文件
// GET /mode4/export/{task_id}
func (s *AppState) mode4ExportTaskHandler(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimPrefix(r.URL.Path, "/mode4/export/")
	if taskID == "" {
		http.Error(w, "任务 ID 为空", http.StatusBadRequest)
		return
	}
	task := s.mode4GetTask(taskID)
	if task == nil {
		http.Error(w, "任务不存在", http.StatusNotFound)
		return
	}

	// 生成文件名
	filename := fmt.Sprintf("号码组合任务_%s.txt", task.CreatedAt)
	if task.Remark != "" {
		clean := strings.Map(func(r rune) rune {
			if r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
				return '_'
			}
			return r
		}, task.Remark)
		if clean != "" {
			filename = clean + ".txt"
		}
	}

	// 更新导出状态
	task.Exported = true
	task.ExportCount++
	task.LastExportedAt = time.Now().Format("2006-01-02 15:04:05")
	_ = s.mode4SaveTask(task)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	// 去掉结果末尾的换行，避免文本编辑器显示额外的空白行。
	w.Write([]byte(strings.TrimRight(task.ResultText, "\r\n")))
}

// mode3ExportSelectedTxtHandler 模式三：批量导出选中 TXT 文件内容
// POST /mode3/export_selected_txt
// Body: {"task_id":"...","selected":[1,2,3]}
// 返回 JSON：每个文件的当前最新内容和展示文件名
func (s *AppState) mode3ExportSelectedTxtHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		TaskID   string `json:"task_id"`
		Selected []int  `json:"selected"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"ok":false,"error":"请求参数解析失败"}`, http.StatusBadRequest)
		return
	}
	if req.TaskID == "" || len(req.Selected) == 0 {
		http.Error(w, `{"ok":false,"error":"参数不完整"}`, http.StatusBadRequest)
		return
	}

	task, err := s.getTask(req.TaskID)
	if err != nil {
		http.Error(w, `{"ok":false,"error":"任务不存在"}`, http.StatusNotFound)
		return
	}

	// 构建选中文件的索引集合
	selectedSet := map[int]bool{}
	for _, idx := range req.Selected {
		if idx > 0 {
			selectedSet[idx] = true
		}
	}

	type fileEntry struct {
		Index    int    `json:"index"`
		Filename string `json:"filename"`
		Content  string `json:"content"`
	}
	var files []fileEntry

	for i := range task.Files {
		f := &task.Files[i]
		if !selectedSet[f.Index] {
			continue
		}
		diskPath, err := s.getFileDiskPath(req.TaskID, *f)
		if err != nil {
			continue // 磁盘文件不存在时跳过
		}
		lines, err := readTextFileLines(diskPath)
		if err != nil {
			continue
		}
		content := strings.Join(lines, "\n")
		files = append(files, fileEntry{
			Index:    f.Index,
			Filename: f.Filename,
			Content:  content,
		})
	}

	resp := struct {
		Ok    bool        `json:"ok"`
		Files []fileEntry `json:"files"`
		Total int         `json:"total"`
	}{
		Ok:    true,
		Files: files,
		Total: len(files),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// shuffleTaskHandler 处理随机打乱请求。
// 对指定任务中指定序号的 TXT 文件执行 Fisher-Yates 随机打乱。
// 请求方式：POST /shuffle_task/{taskID}/{index}
func (s *AppState) shuffleTaskHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/shuffle_task/")
	parts := strings.Split(path, "/")
	if len(parts) < 1 {
		http.Error(w, "参数错误", http.StatusBadRequest)
		return
	}
	taskID := parts[0]
	fileIndex := 1
	if len(parts) >= 2 {
		fileIndex, _ = strconv.Atoi(parts[1])
		if fileIndex < 1 {
			fileIndex = 1
		}
	}

	lock := s.getTaskLock(taskID)
	lock.Lock()
	defer lock.Unlock()

	// 先创建撤回快照
	task, err := s.getTask(taskID)
	if err == nil {
		err = s.createUndoSnapshot(taskID, "随机打乱前")
	}
	if err == nil {
		err = s.shuffleFile(taskID, fileIndex)
	}
	if err == nil {
		_ = s.refreshExportZip(taskID)
		_ = s.saveTaskToDisk(taskID)
		s.setCurrentTask(w, taskID)
		s.logInfo(fmt.Sprintf("已随机打乱文件，索引：%d", fileIndex))
		s.renderMode1(w, r, taskID, task, "已随机打乱 TXT 中的号码顺序。", "")
		return
	}
	s.logError("随机打乱失败：" + err.Error())
	if task != nil {
		s.renderMode1(w, r, taskID, task, "", "随机打乱失败："+err.Error())
		return
	}
	s.render(w, "mode1", pageData{Title: "业务模式一", ActiveNav: "mode1", Error: "随机打乱失败：" + err.Error()})
}

// downloadTxtHandler 直接下载指定 TXT 文件。
// 请求方式：GET /download_txt/{taskID}/{index}
func (s *AppState) downloadTxtHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/download_txt/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		http.Error(w, "参数错误", http.StatusBadRequest)
		return
	}
	taskID := parts[0]
	index, _ := strconv.Atoi(parts[1])

	task, err := s.getTask(taskID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	var item *FileItem
	for i := range task.Files {
		if task.Files[i].Index == index {
			item = &task.Files[i]
			break
		}
	}
	if item == nil {
		http.Error(w, "文件不存在", http.StatusNotFound)
		return
	}
	diskPath, err := s.getFileDiskPath(taskID, *item)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", item.Filename))
	http.ServeFile(w, r, diskPath)
}
