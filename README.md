# GT Split Go

这是基于原 Python/Flask 版 `gt_split_web_full_optimized_v14` 重新搭建的 Go 版本，使用 Go 标准库实现，不依赖第三方包。

## 功能对应

- 业务模式一：上传总数据 TXT，按行数拆分为多个子 TXT。
- 统一插入：支持全部 TXT、按号码平均、指定数量三种策略。
- 插入后自动锁定 TXT，支持一键解锁。
- 插入前自动创建撤回快照，支持撤回上一次插入。
- 支持当前任务持久化、重启恢复、TXT 预览、最新 ZIP 下载、选中 TXT 导出。
- 业务模式二：清洗 `chat.whatsapp.com` 群链接，并提供批量可用性检测接口。
- 业务模式六：上传总数据 TXT/ZIP，按“每个 TXT 数量 × TXT 文件数量”连续取数，记录已取/剩余数量，支持预览历史文件与下载 ZIP。

## 启动

方式一：双击项目根目录里的：

```text
启动GT拆分工具.command
```

方式二：直接运行已编译程序：

```bash
cd /Users/a111/PycharmProjects/fenbao/my_project/文件链接拆分器1.0
./gt_split_go
```

方式三：在终端用源码运行：

```bash
cd /Users/a111/PycharmProjects/fenbao/my_project/文件链接拆分器1.0
go run .
```

启动后会从 `127.0.0.1:8899` 开始寻找空闲端口，并在终端输出访问地址。

## 数据目录

Go 版默认把任务写入：

```text
gt_tasks_go/
```

这样不会覆盖 Python 版原来的 `gt_tasks/` 数据。

## 注意

当前实现只使用 Go 标准库，TXT 会按 UTF-8/有效 UTF-8 文本处理；原 Python 版支持 GBK、GB2312、Big5 等自动解码。如果你需要完全兼容中文旧编码，可以后续加入 `golang.org/x/text/encoding`。
