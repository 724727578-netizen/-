package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var invalidFilenameChars = regexp.MustCompile(`[\\/:*?"<>|]+`)

func safeFilenamePrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	return invalidFilenameChars.ReplaceAllString(prefix, "_")
}

func buildFilename(prefix string, index, width int) string {
	if width < 1 {
		width = 1
	}
	return prefix + leftPadInt(index, width) + ".txt"
}

func leftPadInt(value, width int) string {
	raw := strconv.Itoa(value)
	if len(raw) >= width {
		return raw
	}
	return strings.Repeat("0", width-len(raw)) + raw
}

func normalizeTextLine(line string) string {
	line = strings.TrimPrefix(line, "\ufeff")
	line = strings.TrimSpace(line)
	if !utf8.ValidString(line) {
		line = strings.ToValidUTF8(line, "")
	}
	return line
}

func readNonEmptyLines(r io.Reader) ([]string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var lines []string
	for scanner.Scan() {
		line := normalizeTextLine(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

func splitNonEmptyLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	parts := strings.Split(text, "\n")
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		line := normalizeTextLine(part)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func writeLinesToTextFile(lines []string, path string) (int, string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return 0, "", err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return 0, "", err
	}
	count := 0
	var preview strings.Builder
	previewRuneCount := 0
	for _, line := range lines {
		line = normalizeTextLine(line)
		if line == "" {
			continue
		}
		// 只在两条有效数据之间写换行，最后一条数据末尾不追加换行符。
		if count > 0 {
			if _, err := f.WriteString("\n"); err != nil {
				_ = f.Close()
				_ = os.Remove(tmp)
				return 0, "", err
			}
		}
		if _, err := f.WriteString(line); err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
			return 0, "", err
		}
		count++
		if previewRuneCount < filePreviewChars {
			preview.WriteString(line)
			preview.WriteByte('\n')
			previewRuneCount += utf8.RuneCountInString(line) + 1
		}
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return 0, "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return 0, "", err
	}
	result := preview.String()
	if previewRuneCount > filePreviewChars {
		runes := []rune(result)
		result = string(runes[:filePreviewChars])
	}
	return count, result, nil
}

func readTextPreview(path string, limit int) string {
	if limit <= 0 {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, limit*4)
	n, _ := f.Read(buf)
	text := strings.ToValidUTF8(string(buf[:n]), "")
	runes := []rune(text)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return text
}

func readTextFile(path string, maxChars int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	limit := int64(maxChars * 4)
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return "", err
	}
	text := strings.ToValidUTF8(string(data), "")
	runes := []rune(text)
	if len(runes) > maxChars {
		return string(runes[:maxChars]) + "\n\n... 文件很大，页面仅显示前面部分。请下载 ZIP 获取完整内容。", nil
	}
	return text, nil
}

func parsePositiveInt(raw string, fallback int, field string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s必须大于 0", field)
	}
	return value, nil
}

func withSuffix(lines []string, suffix string) []string {
	if suffix == "" {
		return lines
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, line+suffix)
	}
	return out
}
