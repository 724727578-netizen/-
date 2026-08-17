// types.go — 核心数据类型
//
// 定义本工具所有数据结构：子文件、任务、历史摘要、应用状态。
// host/port/dataDir 等部署配置已移入 config.go，通过环境变量管理。

package main

import "sync"

// ============================================================
// 内部常量
// ============================================================

const (
	// maxUploadBytes 单次上传最大字节数（200MB）。
	// Go 标准库默认 10MB，这里加大到 200MB，避免大 TXT 上传被截断。
	maxUploadBytes = 200 * 1024 * 1024

	// filePreviewChars 页面预览时每个 TXT 最多显示前多少字符。
	// 不设太大，避免页面加载卡顿。
	filePreviewChars = 5000

	// taskStoreName 任务数据存放的子目录名。
	// 完整路径由 BaseDir + taskStoreName 拼接而成。
	taskStoreName = "gt_tasks_go"

	// maxLogLines 运行日志最多保留多少行，超过自动丢弃最早的行。
	maxLogLines = 100
)

// ============================================================
// FileItem — TXT 子文件
// ============================================================

// FileItem 表示拆分出来的一个 TXT 子文件。
// 业务里所有插入、锁定、预览、导出，最终都是围绕这些子文件进行。
type FileItem struct {
	Index         int      `json:"index"`                     // 页面展示和导出选择用的序号，从 1 开始
	Filename      string   `json:"filename"`                  // 用户看到的文件名，例如 "data_01.txt"
	StoredName    string   `json:"stored_name"`               // 磁盘真实保存名，带序号前缀，避免重名覆盖
	LineCount     int      `json:"line_count"`                // 当前 TXT 总行数，插入后会重新统计
	Preview       string   `json:"preview"`                   // 预览文本（仅保存开头部分，完整内容按需读取）
	Locked        bool     `json:"locked"`                    // 已插入的文件会锁定，避免重复操作
	Inserted      bool     `json:"inserted"`                  // 是否执行过插入操作
	InsertedCount int      `json:"inserted_count"`            // 最近一次向这个 TXT 插入了多少条号码
	InsertedLines []string `json:"inserted_lines,omitempty"`  // 实际插入的号码列表，用于页面上展示"插入号码"列
	BusinessMode  string   `json:"business_mode"`             // 最近一次处理方式，例如"统一面板-全部TXT"
	Shuffled      bool     `json:"shuffled"`                  // 是否已执行过随机打乱
	ShuffledAt    string   `json:"shuffled_at,omitempty"`     // 最后打乱时间
}

// pageData — 页面渲染数据
type pageData struct {
	Title        string
	ActiveNav    string
	Error        string
	Success      string
	CurrentID    string
	Summaries    []TaskSummary
	Task         *Task
	TaskID       string
	Logs         string
	TotalFiles   int
	Locked       int
	Unlocked     int
	TotalLines   int
	HasUndo      bool
	ScrollTarget string // 操作完成后滚动到此锚点
	Mode4TaskList string // JSON 序列化的模式四任务列表
	Mode4TaskData string // JSON 序列化的当前打开的模式四任务
}

// ============================================================
// ZipInfo — ZIP 导出记录
// ============================================================

// ZipInfo 记录当前任务最新导出的 ZIP 文件信息。
type ZipInfo struct {
	Name string `json:"name"` // ZIP 文件名，例如 "7-9_VIP_20260709.zip"
	Path string `json:"path"` // ZIP 文件在磁盘上的绝对路径
}

// ============================================================
// Task — 拆分任务
// ============================================================

// Task 表示一次完整的 TXT 拆分任务。
// 一个任务包含多个 TXT 子文件、导出 ZIP、创建时间等信息。
type Task struct {
	TaskID     string     `json:"task_id"`      // 唯一标识，也是磁盘上的目录名（UUID 格式）
	CreatedAt  string     `json:"created_at"`   // 创建时间，格式 "2006-01-02 15:04"
	FilePrefix string     `json:"file_prefix"`  // 文件前缀名，例如"7-9 VIP"，用于生成子文件名
	IndexWidth int        `json:"index_width"`  // 序号补零位数，例如 3 表示生成 "前缀_001.txt"
	Files      []FileItem `json:"files"`        // 该任务包含的所有 TXT 子文件
	EntryMode  string     `json:"entry_mode"`   // 任务创建方式："manual"（手动粘贴）或 "upload"（上传文件）
	ExportZip  *ZipInfo   `json:"export_zip"`   // 最近一次导出的 ZIP，nil 表示尚未导出
	TaskMode   string     `json:"task_mode"`    // 任务类型："split"（按行数拆分）或 "no_split"（不拆分）
}

// ============================================================
// taskPayload — task.json 序列化结构
// ============================================================

// taskPayload 是写入磁盘 task.json 时的外层包装。
// 添加版本号字段预留，方便以后数据结构升级时做兼容处理。
type taskPayload struct {
	TaskID  string `json:"task_id"`            // 任务 ID，与目录名一致
	SavedAt string `json:"saved_at"`           // 保存时间戳
	Task    *Task  `json:"task"`               // 实际任务数据
}

// ============================================================
// TaskSummary — 首页历史任务摘要
// ============================================================

// TaskSummary 用于首页展示历史任务列表。
// 只包含统计摘要，不包含子文件明细，减轻首页加载量。
type TaskSummary struct {
	TaskID        string   // 任务 ID
	ShortID       string   // 任务 ID 前 8 位，页面显示用
	CreatedAt     string   // 创建时间
	LastModified  string   // 最近修改时间（取自磁盘 task.json 的 mtime）
	FilePrefix    string   // 文件前缀名
	TotalFiles    int      // TXT 子文件总数
	LockedCount   int      // 已锁定的子文件数
	UnlockedCount int      // 未锁定的子文件数
	TotalLines    int      // 所有子文件总行数
	IsCurrent     bool     // 是否当前正在操作的任务
	TaskMode      string   // 任务类型："split" 或 "no_split"
	EntryMode     string   // 所属模式："mode1" / "mode2" / "mode3"
}

// ============================================================
// ============================================================
// Mode4Group — 模式四：多组号码组合中的单个组
// ============================================================

// Mode4Group 表示一组号码
type Mode4Group struct {
	GroupIndex int      `json:"group_index"`
	GroupName  string   `json:"group_name"`
	Mode       string   `json:"mode"`       // "align"/"fixed"/"repeat"/"batch"/"random"
	RawText    string   `json:"raw_text"`
	Lines      []string `json:"lines"`
	LineCount  int      `json:"line_count"`

	// repeat 模式：一个号码插入多个小组
	InsertGroupCount int `json:"insert_group_count,omitempty"` // 每个号码插入几个小组，默认 1

	// batch 模式：多个号码插入多个小组（分批）
	BatchNumberCount int `json:"batch_number_count,omitempty"` // 每批号码数量，默认 1
	BatchGroupCount  int `json:"batch_group_count,omitempty"`  // 每批插入小组数，默认 1

	// random 模式：随机插入
	RandInsertCount int  `json:"rand_insert_count,omitempty"` // 每个小组随机插入几个号码，默认 1
	AllowRepeat     bool `json:"allow_repeat,omitempty"`      // 是否允许重复使用号码
}

// ============================================================
// Mode4Task — 模式四：多组号码组合任务
// ============================================================

// Mode4Task 表示一个组合任务
type Mode4Task struct {
	TaskID         string       `json:"task_id"`
	Remark         string       `json:"remark"`
	CreatedAt      string       `json:"created_at"`
	UpdatedAt      string       `json:"updated_at"`
	Groups         []Mode4Group `json:"groups"`
	ResultText     string       `json:"result_text"`
	GroupCount     int          `json:"group_count"`
	ResultGroups   int          `json:"result_groups"`
	Exported       bool         `json:"exported"`
	ExportCount    int          `json:"export_count"`
	LastExportedAt string       `json:"last_exported_at,omitempty"`
}

// ============================================================
// AppState — 应用全局状态
// ============================================================
// ============================================================

// AppState 保存整个 Web 程序运行期间的共享状态。
//
// 注意：多个浏览器请求可能同时操作同一任务，所以任务表和日志都需要锁保护。
// 任务级互斥锁 (locks) 防止用户连续点击导致同一任务并发写入出错。
type AppState struct {
	BaseDir      string            // 程序运行目录（或环境变量 GT_DATA_DIR 指定的目录）
	TaskStoreDir string            // 任务数据目录，完整路径（BaseDir/gt_tasks_go）

	// tasks 是内存中的任务表；task.json 是磁盘持久化副本。
	// 读取时用 RLock，写入时用 Lock。
	mu    sync.RWMutex
	tasks map[string]*Task

	// locks 是任务级互斥锁，防止同一任务被并发操作。
	// 使用前先 getTaskLock(taskID) 获取，操作完后 unlock。
	lockGuard sync.Mutex
	locks     map[string]*sync.Mutex

	// logs 是页面底部"运行日志"的内容，带行数上限，超过自动丢弃。
	logMu sync.Mutex
	logs  []string
}
