// main.go — 程序入口
//
// 负责：初始化环境 → 加载配置 → 恢复数据 → 启动 HTTP 服务。
// 部署配置（host/port/dataDir）通过环境变量管理，详见 config.go。
//
// 使用方式：
//   本地使用：双击可执行文件（.exe / .app / 命令行），自动打开浏览器。
//   服务器部署：设置 GT_HOST / GT_PORT / GT_DATA_DIR 环境变量。

package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

func main() {
	fmt.Println("========== GT Split Go 启动检查 ==========")

	// --------------------------------------------------
	// 第 1 步：确定程序运行目录
	// --------------------------------------------------
	// 双击可执行文件启动时，工作目录就是程序所在目录。
	// 如果 dataDir 环境变量已指定，优先用它作为数据根目录。
	baseDir, err := os.Getwd()
	if err != nil {
		startupFail("无法读取当前目录", err, "请确认不是从压缩包预览窗口里直接运行。")
		os.Exit(1)
	}
	fmt.Println("启动目录：", baseDir)

	// 如果环境变量 GT_DATA_DIR 指定了独立数据目录，以此为准
	rootDir := baseDir
	if dataDir != "" {
		rootDir = dataDir
		fmt.Println("数据目录（来自 GT_DATA_DIR）：", rootDir)
	} else {
		fmt.Println("数据目录（程序目录内）：", filepath.Join(rootDir, taskStoreName))
	}

	// 初始化应用状态（任务表、日志等）
	state := NewAppState(rootDir)

	// --------------------------------------------------
	// 第 2 步：确保任务数据目录存在
	// --------------------------------------------------
	storePath := filepath.Join(rootDir, taskStoreName)
	if err := os.MkdirAll(storePath, 0755); err != nil {
		startupFail("无法创建任务数据目录", err,
			"请检查当前文件夹是否有写入权限，或通过 GT_DATA_DIR 指定一个有权限的目录。")
		os.Exit(1)
	}
	state.logInfo("启动步骤 1/4：任务目录检查完成。")
	mode4Dir := filepath.Join(storePath, "mode4_tasks")
	if err := os.MkdirAll(mode4Dir, 0755); err != nil {
		startupFail("无法创建模式四数据目录", err,
			"请检查当前文件夹是否有写入权限。")
		os.Exit(1)
	}
	state.logInfo("模式四数据目录已就绪。")
	mode6Dir := filepath.Join(storePath, "mode6_tasks")
	if err := os.MkdirAll(mode6Dir, 0755); err != nil {
		startupFail("无法创建模式六数据目录", err,
			"请检查当前文件夹是否有写入权限。")
		os.Exit(1)
	}
	state.logInfo("模式六数据目录已就绪。")

	// --------------------------------------------------
	// 第 3 步：从磁盘恢复历史任务
	// --------------------------------------------------
	state.loadTasksFromDisk()
	state.logInfo("启动步骤 2/4：历史任务恢复完成。")

	// --------------------------------------------------
	// 第 4 步：确定端口并启动 HTTP 服务
	// --------------------------------------------------
	// 本地使用：GT_PORT=8899，如果被占用自动往后找。
	// 服务器部署：建议固定端口，配合 Nginx 反向代理。
	port, err := findFreePort(serverPort, 2000)
	if err != nil {
		startupFail("没有找到可用端口", err,
			"当前端口范围 ("+fmt.Sprint(serverPort)+"-"+fmt.Sprint(serverPort+2000)+") 均被占用。\n"+
				"请通过 GT_PORT 环境变量指定其他端口再试。")
		os.Exit(1)
	}
	state.logInfo(fmt.Sprintf("启动步骤 3/4：端口检查完成，使用端口 %d。", port))

	// 将实际端口写入临时文件，供外部脚本读取
	_ = os.MkdirAll(os.TempDir(), 0755)
	portFile := filepath.Join(os.TempDir(), "gt_split_go_port.txt")
	_ = os.WriteFile(portFile, []byte(fmt.Sprintf("%d", port)), 0644)
	state.logInfo(fmt.Sprintf("端口信息已写入临时文件：%s", portFile))

	// 注册所有 HTTP 路由
	mux := http.NewServeMux()
	registerRoutes(mux, state)
	state.logInfo("说明：这是网页版工具，只处理本机 TXT 文件，不涉及 Telegram 等外部服务。")

	url := fmt.Sprintf("http://%s:%d", serverHost, port)
	state.logInfo(fmt.Sprintf("访问地址：%s", url))

	// HTTP 服务器实例，方便优雅关闭
	srv := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", serverHost, port),
		Handler: mux,
	}

	// --------------------------------------------------
	// 第 5 步：优雅关闭（捕获 SIGINT/SIGTERM）
	// --------------------------------------------------
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-quit
		state.logInfo(fmt.Sprintf("收到关闭信号 (%v)，正在安全退出，最多等待 5 秒...", sig))
		// 清理端口临时文件
		_ = os.Remove(portFile)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			state.logInfo("退出时等待请求处理超时，强制退出。")
		}
	}()

	// --------------------------------------------------
	// 第 6 步：自动打开浏览器（延迟 1 秒等服务就绪）
	// --------------------------------------------------
	if getEnv("GT_NO_BROWSER", "") != "1" {
		go func() {
			time.Sleep(1 * time.Second)
			state.logInfo(fmt.Sprintf("正在自动打开浏览器：%s", url))
			if err := openBrowser(url); err != nil {
				state.logInfo(fmt.Sprintf("自动打开浏览器失败：%v，请手动复制以上地址到浏览器访问。", err))
			}
		}()
	}

	// --------------------------------------------------
	// 第 7 步：正式启动
	// --------------------------------------------------
	state.logInfo(fmt.Sprintf("Web 服务已启动：%s", url))
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		startupFail("Web 服务异常停止", err, "请检查端口是否被其他程序占用。")
		os.Exit(1)
	}

	// 优雅关闭完成
	fmt.Println("程序已安全退出。")
}

// openBrowser 跨平台打开系统默认浏览器。
// Windows: cmd /c start
// macOS: open
// Linux: xdg-open
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("cmd", "/c", "start", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

// startupFail 输出启动失败的详细错误信息，方便用户排查。
func startupFail(title string, err error, hint string) {
	fmt.Println("========== 启动失败 ==========")
	fmt.Println("错误位置：", title)
	fmt.Println("详细原因：", err)
	fmt.Println("处理建议：", hint)
	fmt.Println("如果仍然无法解决，请把本窗口的完整文字截图反馈。")
}

// findFreePort 从 start 开始往后找可用端口。
//
// 本机使用：默认从 8899 开始，被占用就自动 +1，直到找到可用端口。
// 服务器部署：建议固定端口，配合防火墙白名单，此时仅检测一次。
//
// start     — 起始端口（来自 serverPort 配置）
// maxTries  — 最多尝试多少个端口（2000 个足够避免冲突）
func findFreePort(start, maxTries int) (int, error) {
	for port := start; port < start+maxTries; port++ {
		ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", serverHost, port))
		if err == nil {
			_ = ln.Close()
			return port, nil
		}
	}
	return 0, fmt.Errorf("端口范围 %d-%d 均被占用", start, start+maxTries-1)
}
