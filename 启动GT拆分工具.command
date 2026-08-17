#!/bin/zsh

set -u

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
APP_DIR="$SCRIPT_DIR"

clear
echo "========================================"
echo " GT Split Go 本地拆分工具"
echo "========================================"
echo

if [ ! -d "$APP_DIR" ]; then
  echo "启动失败：找不到 Go 项目目录："
  echo "$APP_DIR"
  echo
  echo "请确认 gt_split_go文件链接拆分器 文件夹和本启动文件在同一个目录。"
  echo
  read "REPLY?按回车关闭窗口..."
  exit 1
fi

GO_BIN=""
for CANDIDATE in "$(command -v go 2>/dev/null)" "/opt/homebrew/bin/go" "/usr/local/go/bin/go" "/usr/local/bin/go"; do
  if [ -n "$CANDIDATE" ] && [ -x "$CANDIDATE" ]; then
    GO_BIN="$CANDIDATE"
    break
  fi
done

if [ -z "$GO_BIN" ]; then
  echo "启动失败：当前电脑没有找到 Go 运行环境。"
  echo
  echo "请先安装 Go，然后再双击本文件启动："
  echo "https://go.dev/dl/"
  echo
  echo "安装完成后，如果仍然提示找不到 Go，请重新打开 Finder 后再试。"
  echo
  read "REPLY?按回车关闭窗口..."
  exit 1
fi

cd "$APP_DIR" || exit 1

echo "Go 路径：$GO_BIN"
echo "项目目录：$APP_DIR"
echo
echo "正在启动本地网页服务..."
echo "启动成功后，请复制终端里显示的 http://127.0.0.1:端口 地址到浏览器打开。"
echo

if [ -x "$APP_DIR/gt_split_go" ]; then
  "$APP_DIR/gt_split_go"
else
  echo "未找到已编译程序，自动使用 go run 启动..."
  "$GO_BIN" run .
fi

EXIT_CODE=$?
echo
echo "服务已停止，退出码：$EXIT_CODE"
echo
read "REPLY?按回车关闭窗口..."
exit "$EXIT_CODE"
