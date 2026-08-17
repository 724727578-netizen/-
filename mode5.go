package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const mode5MaxExpandedBytes int64 = 500 * 1024 * 1024
const mode5MaxRequestBytes int64 = maxUploadBytes + 10*1024*1024

type mode5TextFile struct {
	Name  string
	Lines []string
}

func (s *AppState) mode5Handler(w http.ResponseWriter, r *http.Request) {
	s.render(w, "mode5", pageData{Title: "业务模式五 — TXT 汇总与批量插入", ActiveNav: "mode5"})
}

func (s *AppState) renderMode5Error(w http.ResponseWriter, message string) {
	s.logError("模式五：" + message)
	s.render(w, "mode5", pageData{Title: "业务模式五 — TXT 汇总与批量插入", ActiveNav: "mode5", Error: message})
}

func (s *AppState) mode5MergeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, mode5MaxRequestBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		s.renderMode5Error(w, "上传内容解析失败："+err.Error())
		return
	}
	files, err := collectMode5Files(r.MultipartForm, "merge_txt_files", "merge_archive")
	if err != nil {
		s.renderMode5Error(w, err.Error())
		return
	}
	if len(files) == 0 {
		s.renderMode5Error(w, "请上传至少一个 TXT 文件或 ZIP 压缩包")
		return
	}

	var merged []string
	for _, file := range files {
		merged = append(merged, file.Lines...)
	}
	if r.FormValue("deduplicate") == "yes" {
		seen := make(map[string]bool, len(merged))
		unique := make([]string, 0, len(merged))
		for _, line := range merged {
			if !seen[line] {
				seen[line] = true
				unique = append(unique, line)
			}
		}
		merged = unique
	}
	if r.FormValue("sort_lines") == "asc" {
		sort.Strings(merged)
	}
	if len(merged) == 0 {
		s.renderMode5Error(w, "上传文件中没有有效的非空号码")
		return
	}

	filename := "模式五_号码汇总_" + time.Now().Format("20060102_150405") + ".txt"
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	_, _ = io.WriteString(w, strings.Join(merged, "\n"))
	s.logInfo(fmt.Sprintf("模式五汇总完成：读取 %d 个 TXT，输出 %d 条号码。", len(files), len(merged)))
}

func (s *AppState) mode5DirectInsertHandlerLegacy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, mode5MaxRequestBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		s.renderMode5Error(w, "上传内容解析失败："+err.Error())
		return
	}
	files, err := collectMode5Files(r.MultipartForm, "insert_txt_files", "insert_archive")
	if err != nil {
		s.renderMode5Error(w, err.Error())
		return
	}
	if len(files) == 0 {
		s.renderMode5Error(w, "请上传至少一个需要插入号码的 TXT 文件或 ZIP 压缩包")
		return
	}
	numbers := splitNonEmptyLines(r.FormValue("numbers"))
	if len(numbers) == 0 {
		s.renderMode5Error(w, "请在号码输入框中填写至少一条有效号码")
		return
	}
	numbers = withSuffix(numbers, strings.TrimSpace(r.FormValue("number_suffix")))
	position := r.FormValue("insert_position")
	if position != "bottom" {
		position = "top"
	}
	rule := r.FormValue("insert_rule")
	if rule == "" {
		rule = "same_all"
	}

	insertedFiles := 0
	apply := func(index int, picked []string) {
		if len(picked) == 0 {
			return
		}
		if position == "top" {
			files[index].Lines = append(append([]string(nil), picked...), files[index].Lines...)
		} else {
			files[index].Lines = append(files[index].Lines, picked...)
		}
		insertedFiles++
	}

	switch rule {
	case "same_all":
		for i := range files {
			apply(i, numbers)
		}
	case "sequential":
		perFile, parseErr := mode5PositiveInt(r.FormValue("per_file_count"), 1, "每个 TXT 插入数量")
		if parseErr != nil {
			s.renderMode5Error(w, parseErr.Error())
			return
		}
		needed := len(files) * perFile
		if len(numbers) < needed {
			s.renderMode5Error(w, fmt.Sprintf("号码不足：%d 个 TXT 每个需要 %d 条，共需要 %d 条，当前只有 %d 条", len(files), perFile, needed, len(numbers)))
			return
		}
		cursor := 0
		for i := range files {
			apply(i, numbers[cursor:cursor+perFile])
			cursor += perFile
		}
	case "per_number":
		fileCount, parseErr := mode5PositiveInt(r.FormValue("files_per_number"), 1, "每个号码进入文件数量")
		if parseErr != nil {
			s.renderMode5Error(w, parseErr.Error())
			return
		}
		needed := len(numbers) * fileCount
		if needed > len(files) {
			s.renderMode5Error(w, fmt.Sprintf("文件不足：%d 条号码每条进入 %d 个 TXT，需要 %d 个 TXT，当前只有 %d 个", len(numbers), fileCount, needed, len(files)))
			return
		}
		cursor := 0
		for _, number := range numbers {
			for i := 0; i < fileCount; i++ {
				apply(cursor, []string{number})
				cursor++
			}
		}
	case "first_n":
		target, parseErr := mode5PositiveInt(r.FormValue("target_file_count"), 1, "目标 TXT 数量")
		if parseErr != nil {
			s.renderMode5Error(w, parseErr.Error())
			return
		}
		if target > len(files) {
			target = len(files)
		}
		for i := 0; i < target; i++ {
			apply(i, numbers)
		}
	default:
		s.renderMode5Error(w, "未知的插入规则")
		return
	}

	filename := "模式五_批量插入结果_" + time.Now().Format("20060102_150405") + ".zip"
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	zw := zip.NewWriter(w)
	for _, file := range files {
		entry, createErr := zw.Create(file.Name)
		if createErr != nil {
			_ = zw.Close()
			return
		}
		_, _ = io.WriteString(entry, strings.Join(file.Lines, "\n"))
	}
	_ = zw.Close()
	s.logInfo(fmt.Sprintf("模式五批量插入完成：共输出 %d 个 TXT，其中 %d 个执行了插入。", len(files), insertedFiles))
}

func collectMode5Files(form *multipart.Form, textField, archiveField string) ([]mode5TextFile, error) {
	if form == nil {
		return nil, nil
	}
	var files []mode5TextFile
	usedNames := map[string]int{}
	var expandedBytes int64

	addText := func(name string, data []byte) error {
		expandedBytes += int64(len(data))
		if expandedBytes > mode5MaxExpandedBytes {
			return fmt.Errorf("解压和读取后的 TXT 总大小不能超过 500MB")
		}
		name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
		if !strings.EqualFold(filepath.Ext(name), ".txt") {
			return nil
		}
		if name == "" || name == ".txt" {
			name = "data.txt"
		}
		name = uniqueMode5Name(name, usedNames)
		files = append(files, mode5TextFile{Name: name, Lines: splitNonEmptyLines(string(data))})
		return nil
	}

	for _, header := range form.File[textField] {
		if header == nil || header.Filename == "" {
			continue
		}
		data, err := readMode5Upload(header, maxUploadBytes)
		if err != nil {
			return nil, err
		}
		if err := addText(header.Filename, data); err != nil {
			return nil, err
		}
	}
	for _, header := range form.File[archiveField] {
		if header == nil || header.Filename == "" {
			continue
		}
		if !strings.EqualFold(filepath.Ext(header.Filename), ".zip") {
			return nil, fmt.Errorf("压缩包只支持 ZIP 格式：%s", header.Filename)
		}
		data, err := readMode5Upload(header, maxUploadBytes)
		if err != nil {
			return nil, err
		}
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, fmt.Errorf("ZIP 无法读取：%s", header.Filename)
		}
		entries := append([]*zip.File(nil), zr.File...)
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
		for _, entry := range entries {
			if entry.FileInfo().IsDir() || !strings.EqualFold(filepath.Ext(entry.Name), ".txt") {
				continue
			}
			remaining := mode5MaxExpandedBytes - expandedBytes
			if remaining <= 0 || entry.UncompressedSize64 > uint64(remaining) {
				return nil, fmt.Errorf("ZIP 解压后的 TXT 总大小不能超过 500MB")
			}
			rc, openErr := entry.Open()
			if openErr != nil {
				return nil, openErr
			}
			entryData, readErr := io.ReadAll(io.LimitReader(rc, remaining+1))
			_ = rc.Close()
			if readErr != nil {
				return nil, readErr
			}
			if int64(len(entryData)) > remaining {
				return nil, fmt.Errorf("ZIP 解压后的 TXT 总大小不能超过 500MB")
			}
			if err := addText(entry.Name, entryData); err != nil {
				return nil, err
			}
		}
	}
	return files, nil
}

func readMode5Upload(header *multipart.FileHeader, limit int64) ([]byte, error) {
	if header == nil || header.Filename == "" {
		return nil, nil
	}
	file, err := header.Open()
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("单个上传文件不能超过 %dMB", limit/(1024*1024))
	}
	return data, nil
}

func uniqueMode5Name(name string, used map[string]int) string {
	key := strings.ToLower(name)
	if used[key] == 0 {
		used[key] = 1
		return name
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := used[key] + 1; ; i++ {
		candidate := base + "_" + strconv.Itoa(i) + ext
		candidateKey := strings.ToLower(candidate)
		if used[candidateKey] == 0 {
			used[key] = i
			used[candidateKey] = 1
			return candidate
		}
	}
}

func mode5PositiveInt(raw string, fallback int, field string) (int, error) {
	value, err := parsePositiveInt(raw, fallback, field)
	if err != nil {
		return 0, err
	}
	return value, nil
}
