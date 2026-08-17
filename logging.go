package main

import (
	"fmt"
	"strings"
	"time"
)

// addLog 同时写入终端和页面日志。
// 这样用户截图终端或查看网页底部日志时，都能看到同一条问题线索。
func (s *AppState) addLog(level, msg string) {
	line := fmt.Sprintf("[%s] [%s] [GT_SPLIT_GO] %s", time.Now().Format("2006-01-02 15:04:05"), level, msg)
	s.logMu.Lock()
	s.logs = append(s.logs, line)
	// 日志最多保留 600 行，避免工具长时间运行后页面越来越慢。
	if len(s.logs) > 600 {
		s.logs = append([]string(nil), s.logs[len(s.logs)-600:]...)
	}
	s.logMu.Unlock()
	fmt.Println(line)
}

func (s *AppState) logInfo(msg string)  { s.addLog("信息", msg) }
func (s *AppState) logWarn(msg string)  { s.addLog("警告", msg) }
func (s *AppState) logError(msg string) { s.addLog("错误", msg) }

// logErrorHint 用于用户操作失败时记录“原因 + 建议”。
// 看到这类日志时，优先按“处理建议”检查输入或文件状态。
func (s *AppState) logErrorHint(action string, err error, hint string) {
	s.logError(fmt.Sprintf("%s失败：%v", action, err))
	if hint != "" {
		s.logWarn("处理建议：" + hint)
	}
}

func (s *AppState) clearLogs() {
	s.logMu.Lock()
	s.logs = nil
	s.logMu.Unlock()
}

func (s *AppState) logsText() string {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	return strings.Join(s.logs, "\n")
}
