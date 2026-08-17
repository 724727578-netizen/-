package main

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (s *AppState) createTaskFromRequest(r *http.Request) (string, error) {
	// 创建任务必须读取上传的总 TXT，所以这里使用 multipart 表单解析。
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		return "", err
	}
	file, header, err := r.FormFile("main_txt")
	if err != nil {
		return "", fmt.Errorf("请上传总数据包 TXT 文件")
	}
	defer file.Close()

	taskMode := r.FormValue("task_mode")
	if taskMode == "" {
		taskMode = "split"
	}

	filePrefix := safeFilenamePrefix(r.FormValue("file_prefix"))
	if filePrefix == "" {
		filePrefix = "data_"
	}

	lines, err := readNonEmptyLines(file)
	if err != nil {
		return "", err
	}
	if len(lines) == 0 {
		return "", fmt.Errorf("总数据包没有有效内容")
	}

	taskID := newTaskID()
	if err := initTaskDirs(s.taskFilesDir(taskID), s.taskZipDir(taskID)); err != nil {
		return "", err
	}

	var generated []FileItem

	if taskMode == "no_split" {
		// ========== 不拆分模式 ==========
		// 使用上传文件的原文件名作为用户看到的文件名
		originalName := "data.txt"
		if header != nil && header.Filename != "" {
			originalName = header.Filename
		}
		// 磁盘内部使用安全存储名避免重名
		storedName := makeStoredName(1, originalName)
		path := s.taskFilePath(taskID, storedName)
		lineCount, preview, err := writeLinesToTextFile(lines, path)
		if err != nil {
			return "", err
		}
		generated = append(generated, FileItem{
			Index:      1,
			Filename:   originalName,
			StoredName: storedName,
			LineCount:  lineCount,
			Preview:    preview,
		})
		s.logInfo("已生成文件：" + originalName)
	} else {
		// ========== 按行数拆分模式（原有逻辑） ==========
		splitSize, err := parsePositiveInt(r.FormValue("split_size"), 0, "拆分行数")
		if err != nil {
			return "", err
		}
		indexWidth, err := parsePositiveInt(r.FormValue("index_width"), 2, "编号位数")
		if err != nil {
			return "", err
		}
		keepRemainder := r.FormValue("keep_remainder")
		if keepRemainder == "" {
			keepRemainder = "yes"
		}

		dropped := 0
		for start := 0; start < len(lines); start += splitSize {
			end := start + splitSize
			if end > len(lines) {
				end = len(lines)
			}
			chunk := lines[start:end]
			if len(chunk) < splitSize && keepRemainder == "no" {
				dropped = len(chunk)
				break
			}
			index := len(generated) + 1
			filename := buildFilename(filePrefix, index, indexWidth)
			storedName := makeStoredName(index, filename)
			path := s.taskFilePath(taskID, storedName)
			lineCount, preview, err := writeLinesToTextFile(chunk, path)
			if err != nil {
				return "", err
			}
			generated = append(generated, FileItem{
				Index:      index,
				Filename:   filename,
				StoredName: storedName,
				LineCount:  lineCount,
				Preview:    preview,
			})
			s.logInfo("已生成文件：" + filename)
		}
		if len(generated) == 0 {
			return "", fmt.Errorf("拆分后没有可展示的子文件")
		}
		if dropped > 0 {
			s.logWarn(fmt.Sprintf("总数据包尾包丢弃：%d 行。", dropped))
		}
	}

	if header != nil {
		s.logInfo(fmt.Sprintf("总数据包：%s，有效数据：%d 行", header.Filename, len(lines)))
	} else {
		s.logInfo(fmt.Sprintf("总数据包有效数据：%d 行", len(lines)))
	}

	task := &Task{
		TaskID:     taskID,
		CreatedAt:  time.Now().Format("2006-01-02 15:04:05"),
		FilePrefix: filePrefix,
		IndexWidth: 2,
		Files:      generated,
		EntryMode:  "mode1",
		TaskMode:   taskMode,
	}
	if taskMode == "split" {
		task.IndexWidth, _ = parsePositiveInt(r.FormValue("index_width"), 2, "编号位数")
	}
	s.addTask(task)
	if err := s.refreshExportZip(taskID); err != nil {
		return "", err
	}
	return taskID, nil
}

func availableFileIndexes(files []FileItem) []int {
	var indexes []int
	for i := range files {
		// 只处理未锁定文件；锁定文件代表已经插入过，避免重复处理。
		if !files[i].Locked {
			indexes = append(indexes, i)
		}
	}
	return indexes
}

func linesFromUploadOrPaste(r *http.Request, fieldName, pasteName string) ([]string, string, error) {
	// 兼容两种来源：优先读取上传 TXT；没有上传时再读取页面粘贴内容。
	if r.MultipartForm != nil && r.MultipartForm.File != nil {
		headers := r.MultipartForm.File[fieldName]
		for _, header := range headers {
			if header == nil || header.Filename == "" {
				continue
			}
			file, err := header.Open()
			if err != nil {
				return nil, "", err
			}
			defer file.Close()
			lines, err := readNonEmptyLines(file)
			if err != nil {
				return nil, "", err
			}
			if len(lines) > 0 {
				return lines, "上传文件：" + header.Filename, nil
			}
		}
	}
	pasteText := strings.TrimSpace(r.FormValue(pasteName))
	if pasteText != "" {
		lines := splitNonEmptyLines(pasteText)
		if len(lines) > 0 {
			return lines, "页面粘贴内容", nil
		}
	}
	return nil, "", fmt.Errorf("请上传插入数据 TXT，或者直接粘贴插入内容")
}

func (s *AppState) runMode1Insert(taskID string, task *Task, r *http.Request) (string, error) {
	// 旧版兼容插入支持上传 TXT，所以必须按 multipart 解析。
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		return "", err
	}
	insertCount, err := parsePositiveInt(r.FormValue("insert_count"), 1, "每个文件插入数量")
	if err != nil {
		return "", err
	}
	insertPosition := r.FormValue("insert_position")
	if insertPosition == "" {
		insertPosition = "top"
	}
	insertMode := r.FormValue("insert_mode")
	if insertMode == "" {
		insertMode = "sequential"
	}
	lines, sourceName, err := linesFromUploadOrPaste(r, "insert_txt", "paste_text")
	if err != nil {
		return "", err
	}
	lines = withSuffix(lines, strings.TrimSpace(r.FormValue("number_suffix")))
	available := availableFileIndexes(task.Files)
	if len(available) == 0 {
		return "", fmt.Errorf("没有可插入的文件，所有文件都已封装锁定")
	}
	s.logInfo("模式一数据来源：" + sourceName)

	if insertMode == "same_for_all" {
		if len(lines) < insertCount {
			return "", fmt.Errorf("插入数据不足，共需要 %d 条，当前只有 %d 条", insertCount, len(lines))
		}
		picked := lines[:insertCount]
		for _, idx := range available {
			if err := s.rewriteItemWithInsert(taskID, &task.Files[idx], picked, insertPosition, "模式一", insertCount); err != nil {
				return "", err
			}
			s.logInfo(fmt.Sprintf("%s 已完成模式一插入并锁定，本次插入 %d 条。", task.Files[idx].Filename, insertCount))
		}
	} else {
		needed := len(available) * insertCount
		if len(lines) < needed {
			return "", fmt.Errorf("插入数据不足，共需要 %d 条，当前只有 %d 条", needed, len(lines))
		}
		cursor := 0
		for _, idx := range available {
			picked := lines[cursor : cursor+insertCount]
			cursor += insertCount
			if err := s.rewriteItemWithInsert(taskID, &task.Files[idx], picked, insertPosition, "模式一", insertCount); err != nil {
				return "", err
			}
			s.logInfo(fmt.Sprintf("%s 已完成模式一插入并锁定，本次插入 %d 条。", task.Files[idx].Filename, insertCount))
		}
	}
	if err := s.refreshExportZip(taskID); err != nil {
		return "", err
	}
	return "业务模式一插入完成，文件已锁定，不能二次插入。", nil
}

func (s *AppState) runUnifiedInsert(taskID string, task *Task, r *http.Request) (string, error) {
	strategy := r.FormValue("unified_strategy")
	if strategy == "" {
		strategy = "all_txt"
	}
	// 同时检查文件上传和手动粘贴，只能选择一种数据来源。
	hasFile := r.MultipartForm != nil && len(r.MultipartForm.File["insert_txt"]) > 0
	hasPaste := strings.TrimSpace(r.FormValue("paste_text")) != ""
	if hasFile && hasPaste {
		return "", fmt.Errorf("你同时上传了 TXT 文件并填写了粘贴内容，请只保留一种插入数据来源")
	}
	lines, sourceName, err := linesFromUploadOrPaste(r, "insert_txt", "paste_text")
	if err != nil {
		return "", err
	}
	lines = withSuffix(lines, strings.TrimSpace(r.FormValue("number_suffix")))
	if len(lines) == 0 {
		return "", fmt.Errorf("插入数据没有有效内容")
	}
	s.logInfo("统一插入数据来源：" + sourceName)
	insertCount, err := parsePositiveInt(r.FormValue("insert_count"), 1, "每个文件插入数量")
	if err != nil {
		return "", err
	}
	perNumberFileCount, err := parsePositiveInt(r.FormValue("per_number_file_count"), 1, "每个号码进入 TXT 数量")
	if err != nil {
		return "", err
	}
	targetFileCount, err := parsePositiveInt(r.FormValue("target_file_count"), 1, "目标 TXT 数量")
	if err != nil {
		return "", err
	}
	insertPosition := r.FormValue("insert_position")
	if insertPosition == "" {
		insertPosition = "top"
	}
	insertMode := r.FormValue("insert_mode")
	if insertMode == "" {
		insertMode = "same_for_all"
	}
	available := availableFileIndexes(task.Files)
	if len(available) == 0 {
		return "", fmt.Errorf("没有可插入的文件，所有 TXT 文件都已锁定")
	}
	businessMode := map[string]string{
		"all_txt":        "统一面板-全部TXT",
		"per_number":     "统一面板-按号码平均",
		"group_to_count": "统一面板-一组到指定TXT",
	}[strategy]
	if businessMode == "" {
		businessMode = "统一面板"
	}

	var msg string
	switch strategy {
	case "all_txt":
		// 策略一：全部 TXT。
		// same_for_all：每个未锁定 TXT 都插入同一批粘贴内容。
		// sequential：按顺序消耗粘贴内容，每个 TXT 拿 insertCount 条，不重复使用。
		if insertMode == "same_for_all" {
			for _, idx := range available {
				if err := s.rewriteItemWithInsert(taskID, &task.Files[idx], lines, insertPosition, businessMode, len(lines)); err != nil {
					return "", err
				}
				s.logInfo(fmt.Sprintf("%s 已插入全部粘贴号码并锁定，本次插入 %d 条。", task.Files[idx].Filename, len(lines)))
			}
			msg = fmt.Sprintf("已把粘贴的 %d 个号码插入全部 %d 个未锁定 TXT。", len(lines), len(available))
		} else {
			needed := len(available) * insertCount
			if len(lines) < needed {
				return "", fmt.Errorf("插入数据不足，共需要 %d 条，当前只有 %d 条", needed, len(lines))
			}
			cursor := 0
			for _, idx := range available {
				picked := lines[cursor : cursor+insertCount]
				cursor += insertCount
				if err := s.rewriteItemWithInsert(taskID, &task.Files[idx], picked, insertPosition, businessMode, insertCount); err != nil {
					return "", err
				}
				s.logInfo(fmt.Sprintf("%s 已按顺序插入并锁定，本次插入 %d 条。", task.Files[idx].Filename, insertCount))
			}
			msg = fmt.Sprintf("已按顺序给全部 %d 个未锁定 TXT 插入号码，每个 TXT %d 条。", len(available), insertCount)
		}
	case "per_number":
		// 策略二：按号码平均。
		// 一个号码进入 perNumberFileCount 个 TXT，然后下一个号码继续进入下一批 TXT。
		neededFiles := len(lines) * perNumberFileCount
		if neededFiles > len(available) {
			return "", fmt.Errorf("当前粘贴 %d 个号码，每个号码进入 %d 个 TXT，共需要 %d 个未锁定 TXT，当前只有 %d 个", len(lines), perNumberFileCount, neededFiles, len(available))
		}
		cursor := 0
		for _, number := range lines {
			for i := 0; i < perNumberFileCount; i++ {
				idx := available[cursor]
				cursor++
				if err := s.rewriteItemWithInsert(taskID, &task.Files[idx], []string{number}, insertPosition, businessMode, 1); err != nil {
					return "", err
				}
				s.logInfo(fmt.Sprintf("%s 已插入号码并锁定：%s", task.Files[idx].Filename, number))
			}
		}
		msg = fmt.Sprintf("已按号码平均分配：%d 个号码，每个号码进入 %d 个 TXT，共处理 %d 个 TXT。", len(lines), perNumberFileCount, neededFiles)
	case "group_to_count":
		// 策略三：指定数量。
		// 只处理前 targetFileCount 个未锁定 TXT，适合分批推进。
		realCount := targetFileCount
		if realCount > len(available) {
			realCount = len(available)
		}
		targets := available[:realCount]
		if insertMode == "same_for_all" {
			for _, idx := range targets {
				if err := s.rewriteItemWithInsert(taskID, &task.Files[idx], lines, insertPosition, businessMode, len(lines)); err != nil {
					return "", err
				}
				s.logInfo(fmt.Sprintf("%s 已插入整组号码并锁定，本次插入 %d 条。", task.Files[idx].Filename, len(lines)))
			}
			msg = fmt.Sprintf("已把整组 %d 个号码插入到 %d 个 TXT。", len(lines), realCount)
		} else {
			needed := realCount * insertCount
			if len(lines) < needed {
				return "", fmt.Errorf("插入数据不足，共需要 %d 条，当前只有 %d 条", needed, len(lines))
			}
			cursor := 0
			for _, idx := range targets {
				picked := lines[cursor : cursor+insertCount]
				cursor += insertCount
				if err := s.rewriteItemWithInsert(taskID, &task.Files[idx], picked, insertPosition, businessMode, insertCount); err != nil {
					return "", err
				}
				s.logInfo(fmt.Sprintf("%s 已按顺序插入并锁定，本次插入 %d 条。", task.Files[idx].Filename, insertCount))
			}
			msg = fmt.Sprintf("已按顺序把号码插入到 %d 个 TXT，每个 TXT %d 条。", realCount, insertCount)
		}
		if targetFileCount > realCount {
			msg += fmt.Sprintf(" 你要求 %d 个，但当前只剩 %d 个未锁定 TXT。", targetFileCount, realCount)
		}
	default:
		return "", fmt.Errorf("未知的统一插入方式")
	}

	if err := s.refreshExportZip(taskID); err != nil {
		return "", err
	}
	remaining := 0
	for _, item := range task.Files {
		if !item.Locked {
			remaining++
		}
	}
	return fmt.Sprintf("%s 剩余未锁定 TXT：%d 个。", msg, remaining), nil
}

func (s *AppState) rewriteItemWithInsert(taskID string, item *FileItem, picked []string, insertPosition, businessMode string, pickedCount int) error {
	// 改写 TXT 时先写临时文件，成功后再替换原文件，避免中途失败导致原文件损坏。
	originPath, err := s.getFileDiskPath(taskID, *item)
	if err != nil {
		return err
	}
	originFile, err := os.Open(originPath)
	if err != nil {
		return err
	}
	originLines, err := readNonEmptyLines(originFile)
	_ = originFile.Close()
	if err != nil {
		return err
	}
	var merged []string
	if insertPosition == "top" {
		merged = append(merged, picked...)
		merged = append(merged, originLines...)
	} else {
		merged = append(merged, originLines...)
		merged = append(merged, picked...)
	}
	tmp, err := os.CreateTemp(filepath.Dir(originPath), "rewrite_*.txt")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	lineCount, preview, err := writeLinesToTextFile(merged, tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, originPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	item.Preview = preview
	item.LineCount = lineCount
	item.Inserted = true
	item.InsertedCount = pickedCount
	item.InsertedLines = append([]string(nil), picked...)
	item.Locked = true
	item.BusinessMode = businessMode
	return nil
}

func selectedIndexesFromValues(form url.Values) map[int]bool {
	// 页面复选框提交的是 selected_files=序号；这里转成集合，方便导出时快速判断。
	selected := map[int]bool{}
	if form == nil {
		return selected
	}
	for _, raw := range form["selected_files"] {
		value, err := parsePositiveInt(raw, 0, "指定文件")
		if err == nil && value > 0 {
			selected[value] = true
		}
	}
	return selected
}

// shuffleFile 对指定文件执行 Fisher-Yates 随机打乱。
// 只改变行的排列顺序，不修改、不删除、不去重。
func (s *AppState) shuffleFile(taskID string, fileIndex int) error {
	task, err := s.getTask(taskID)
	if err != nil {
		return err
	}
	var item *FileItem
	for i := range task.Files {
		if task.Files[i].Index == fileIndex {
			item = &task.Files[i]
			break
		}
	}
	if item == nil {
		return fmt.Errorf("文件不存在")
	}
	path, err := s.getFileDiskPath(taskID, *item)
	if err != nil {
		return err
	}
	// 读取全部非空行
	lines, err := readTextFileLines(path)
	if err != nil {
		return err
	}
	if len(lines) <= 1 {
		return fmt.Errorf("当前 TXT 只有 %d 条有效数据，无需打乱", len(lines))
	}
	// Fisher-Yates 洗牌算法
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := len(lines) - 1; i > 0; i-- {
		j := rng.Intn(i + 1)
		lines[i], lines[j] = lines[j], lines[i]
	}
	// 写回磁盘
	lineCount, preview, err := writeLinesToTextFile(lines, path)
	if err != nil {
		return err
	}
	item.LineCount = lineCount
	item.Preview = preview
	item.Shuffled = true
	item.ShuffledAt = time.Now().Format("2006-01-02 15:04:05")
	return nil
}

// readTextFileLines 读取文本文件的所有非空行。
func readTextFileLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readNonEmptyLines(f)
}
