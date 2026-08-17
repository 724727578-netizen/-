// config.go — 服务器部署配置
//
// 本文件管理所有可配置的部署参数。
// 所有配置都支持通过环境变量覆盖，无需修改代码即可适配不同环境：
//
//   GT_HOST      监听地址（默认 "127.0.0.1"，服务器部署请设为 "0.0.0.0"）
//   GT_PORT      监听端口（默认 8899）
//   GT_DATA_DIR  数据存放目录（默认 = 程序目录/gt_tasks_go）
//   GT_NO_BROWSER 设为 1 时启动后不自动打开浏览器（适合服务器或自动测试）
//
// 使用示例（Linux 服务器）：
//   export GT_HOST=0.0.0.0
//   export GT_PORT=8899
//   export GT_DATA_DIR=/var/data/gt_tasks
//   nohup ./gt_split_go &

package main

import (
	"os"
	"strconv"
)

// ============================================================
// 服务器配置项
// ============================================================

var (
	// serverHost 监听地址。
	//   - 本机使用：保持 "127.0.0.1"，只有当前电脑能访问。
	//   - 服务器部署：设为 "0.0.0.0"，同网络或公网可以访问。
	// 可通过环境变量 GT_HOST 覆盖。
	serverHost = getEnv("GT_HOST", "127.0.0.1")

	// serverPort 监听端口。
	// 服务器部署时建议固定一个端口，方便 Nginx 反向代理配置。
	// 可通过环境变量 GT_PORT 覆盖。
	serverPort = getEnvInt("GT_PORT", 8899)

	// dataDir 数据存放目录的绝对路径。
	// 为空时使用程序运行目录下的 gt_tasks_go。
	// 服务器部署建议设为独立数据目录，便于备份和数据迁移。
	// 可通过环境变量 GT_DATA_DIR 覆盖。
	dataDir = getEnv("GT_DATA_DIR", "")
)

// ============================================================
// 环境变量读取工具
// ============================================================

// getEnv 读取字符串类型环境变量。
// 如果变量未设置或为空字符串，返回默认值 fallback。
func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// getEnvInt 读取整数类型环境变量。
// 如果变量未设置、为空或格式错误，返回默认值 fallback。
func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
