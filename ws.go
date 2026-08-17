package main

import (
	"crypto/tls"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

var wsLinkRE = regexp.MustCompile(`(?i)^https?://chat\.whatsapp\.com/([A-Za-z0-9_-]{8,80})/?$`)
var wsExtractRE = regexp.MustCompile(`(?i)(?:https?://)?chat\.whatsapp\.com/[A-Za-z0-9_-]{8,80}`)

type wsCheckRequest struct {
	Links []string `json:"links"`
}

type wsCheckResult struct {
	Link      string `json:"link"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	GroupName string `json:"group_name,omitempty"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

func normalizeWSLink(link string) string {
	raw := strings.TrimSpace(link)
	raw = strings.TrimRight(raw, "，。；、,.;:：!！?？)]}>")
	raw = strings.TrimLeft(raw, "<[{(（【")
	if raw == "" {
		return ""
	}
	if !strings.HasPrefix(strings.ToLower(raw), "http://") && !strings.HasPrefix(strings.ToLower(raw), "https://") {
		raw = "https://" + raw
	}
	match := wsLinkRE.FindStringSubmatch(raw)
	if len(match) != 2 {
		return ""
	}
	return "https://chat.whatsapp.com/" + match[1]
}

func extractWSLinks(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, raw := range wsExtractRE.FindAllString(text, -1) {
		link := normalizeWSLink(raw)
		if link != "" && !seen[link] {
			seen[link] = true
			out = append(out, link)
		}
	}
	return out
}

func (s *AppState) mode2CheckLinksHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req wsCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "JSON 参数错误"})
		return
	}
	var links []string
	seen := map[string]bool{}
	for _, raw := range req.Links {
		link := normalizeWSLink(raw)
		if link != "" && !seen[link] {
			seen[link] = true
			links = append(links, link)
		}
	}
	if len(links) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "results": []wsCheckResult{}})
		return
	}
	if len(links) > 80 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "单次最多检测 80 条链接，请分批提交。"})
		return
	}
	s.logInfo("开始检测 WS 群链接可用性。")
	results := make([]wsCheckResult, len(links))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, link := range links {
		wg.Add(1)
		go func(i int, link string) {
			defer wg.Done()
			defer func() {
				if recovered := recover(); recovered != nil {
					results[i] = wsCheckResult{
						Link:    link,
						Status:  "unknown",
						Message: "检测异常，已自动保护不中断程序",
					}
					s.logError("WS 链接检测异常：" + link)
				}
			}()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = checkWSLink(link)
		}(i, link)
	}
	wg.Wait()
	s.logInfo("WS 群链接可用性检测完成。")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "results": results})
}

func checkWSLink(link string) wsCheckResult {
	normalized := normalizeWSLink(link)
	if normalized == "" {
		return wsCheckResult{Link: link, Status: "bad", Message: "链接格式无效"}
	}
	body, statusCode, err := fetchWSPage(normalized, true)
	if err != nil {
		body, statusCode, err = fetchWSPage(normalized, false)
	}
	if err != nil {
		return wsCheckResult{Link: normalized, Status: "unknown", Message: "网络请求失败：" + err.Error()}
	}
	status, message, groupName, avatarURL := classifyWSPage(statusCode, string(body))
	return wsCheckResult{Link: normalized, Status: status, Message: message, GroupName: groupName, AvatarURL: avatarURL}
}

func fetchWSPage(link string, verifySSL bool) ([]byte, int, error) {
	transport := &http.Transport{}
	if !verifySSL {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // 本地检测工具，SSL 失败时兼容旧 Python 行为。
	}
	client := &http.Client{Timeout: 8 * time.Second, Transport: transport}
	req, err := http.NewRequest(http.MethodGet, link, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/125 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	return body, resp.StatusCode, err
}

func classifyWSPage(statusCode int, pageHTML string) (string, string, string, string) {
	lower := strings.ToLower(pageHTML)
	invalidMarkers := []string{
		"invite link is invalid",
		"this invite link is invalid",
		"invite link has been reset",
		"group invite link was reset",
		"couldn't find this group",
		"couldn’t find this group",
		"the invite link you used is invalid",
		"not a valid whatsapp invite link",
	}
	if statusCode == 404 || statusCode == 410 {
		return "bad", http.StatusText(statusCode) + "，链接不存在或已失效", "", ""
	}
	for _, marker := range invalidMarkers {
		if strings.Contains(lower, marker) {
			return "bad", "页面提示邀请链接无效或已重置", "", ""
		}
	}
	groupName, avatarURL := extractWSInviteDetails(pageHTML)
	if statusCode >= 200 && statusCode < 400 {
		if groupName == "" && avatarURL == "" {
			return "bad", "未识别到群名称和群头像，链接失效", groupName, avatarURL
		}
		if groupName != "" && avatarURL != "" {
			return "ok", "可用：识别到群名称“" + groupName + "”和群头像", groupName, avatarURL
		}
		if groupName != "" {
			return "ok", "可用：识别到群名称“" + groupName + "”，未识别到群头像", groupName, avatarURL
		}
		return "ok", "可用：识别到群头像，未识别到群名称", groupName, avatarURL
	}
	return "unknown", "HTTP " + http.StatusText(statusCode) + "，无法确认是否可用", groupName, avatarURL
}

func extractWSInviteDetails(source string) (string, string) {
	var titles []string
	var images []string
	metaRE := regexp.MustCompile(`(?is)<meta\b[^>]*>`)
	for _, tag := range metaRE.FindAllString(source, -1) {
		attrs := parseTagAttrs(tag)
		key := strings.ToLower(firstNonEmpty(attrs["property"], attrs["name"], attrs["itemprop"]))
		content := attrs["content"]
		if content == "" {
			continue
		}
		switch key {
		case "og:title", "twitter:title", "title":
			titles = append(titles, content)
		case "og:image", "twitter:image", "image":
			images = append(images, content)
		}
	}
	titleRE := regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	if match := titleRE.FindStringSubmatch(source); len(match) == 2 {
		titles = append(titles, match[1])
	}
	imgRE := regexp.MustCompile(`(?is)<img\b[^>]*>`)
	for _, tag := range imgRE.FindAllString(source, -1) {
		attrs := parseTagAttrs(tag)
		if src := firstNonEmpty(attrs["src"], attrs["data-src"]); src != "" {
			images = append(images, src)
		}
	}
	groupName := ""
	for _, title := range titles {
		if candidate := extractGroupNameFromTitle(title); candidate != "" {
			groupName = candidate
			break
		}
	}
	avatarURL := ""
	for _, imageURL := range images {
		url := strings.TrimSpace(html.UnescapeString(imageURL))
		lower := strings.ToLower(url)
		if strings.Contains(lower, "pps.whatsapp.net") || strings.Contains(lower, "mmg.whatsapp.net") {
			avatarURL = url
			break
		}
		if strings.Contains(lower, "static.whatsapp.net") || strings.Contains(lower, "whatsapp.com") {
			continue
		}
		if regexp.MustCompile(`(?i)\.(jpg|jpeg|png|webp)(?:\?|$)`).MatchString(lower) {
			avatarURL = url
			break
		}
	}
	return groupName, avatarURL
}

func parseTagAttrs(tag string) map[string]string {
	attrs := map[string]string{}
	// Go 的 regexp 不支持 Python/JS 那种 \1、\2 反向引用。
	// 这里先匹配完整引号值，再手动去掉首尾引号，避免检测 WS 页面时因正则 panic 导致服务退出。
	attrRE := regexp.MustCompile(`(?is)([\w:-]+)\s*=\s*("[^"]*"|'[^']*')`)
	for _, match := range attrRE.FindAllStringSubmatch(tag, -1) {
		if len(match) == 3 {
			value := strings.TrimSpace(match[2])
			if len(value) >= 2 {
				value = value[1 : len(value)-1]
			}
			attrs[strings.ToLower(match[1])] = strings.TrimSpace(html.UnescapeString(value))
		}
	}
	return attrs
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func stripHTMLTags(value string) string {
	re := regexp.MustCompile(`(?is)<[^>]+>`)
	text := re.ReplaceAllString(value, " ")
	text = html.UnescapeString(text)
	return strings.Join(strings.Fields(text), " ")
}

func normalizeWSTitle(value string) string {
	text := stripHTMLTags(value)
	text = regexp.MustCompile(`(?i)^\s*whatsapp\s*[-|:]\s*`).ReplaceAllString(text, "")
	text = regexp.MustCompile(`(?i)\s*[-|:]\s*whatsapp\s*$`).ReplaceAllString(text, "")
	return strings.Join(strings.Fields(text), " ")
}

func isGenericWSTitle(value string) bool {
	title := strings.ToLower(normalizeWSTitle(value))
	if title == "" {
		return true
	}
	generic := map[string]bool{
		"群聊邀请": true, "群組邀請": true, "群组邀请": true,
		"whatsapp group invite": true, "whatsapp group invitation": true,
		"whatsapp group chat invite": true, "whatsapp group chat invitation": true,
		"group invite": true, "group invitation": true, "group chat invite": true,
		"group chat invitation": true, "chat invite": true, "whatsapp": true,
	}
	return generic[title]
}

func extractGroupNameFromTitle(value string) string {
	title := normalizeWSTitle(value)
	if title == "" || isGenericWSTitle(title) {
		return ""
	}
	cleaned := title
	patterns := []string{
		`\s*[-|:：]?\s*群聊邀请\s*$`,
		`\s*[-|:：]?\s*群組邀請\s*$`,
		`\s*[-|:：]?\s*群组邀请\s*$`,
		`\s*[-|:：]?\s*group\s+chat\s+invitation\s*$`,
		`\s*[-|:：]?\s*group\s+chat\s+invite\s*$`,
		`\s*[-|:：]?\s*group\s+invitation\s*$`,
		`\s*[-|:：]?\s*group\s+invite\s*$`,
	}
	for _, pattern := range patterns {
		cleaned = regexp.MustCompile(`(?i)`+pattern).ReplaceAllString(cleaned, "")
		cleaned = strings.TrimSpace(cleaned)
	}
	if cleaned == "" || isGenericWSTitle(cleaned) {
		return ""
	}
	return cleaned
}
