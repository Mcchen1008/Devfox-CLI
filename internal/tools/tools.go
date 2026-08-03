package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// 工具结果截断保护：防止超大输出撑爆 API 请求
const MaxToolResult = 200 * 1024

// ToolDef 工具定义（对应 C 版 tool_def_t）
type ToolDef struct {
	Name        string
	Description string
	Parameters  string // JSON Schema 字符串
	Fn          func(args map[string]any) string
}

func truncateResult(s string) string {
	if len(s) <= MaxToolResult {
		return s
	}
	return s[:MaxToolResult] + fmt.Sprintf("\n...（输出过长，已截断至 %d 字节）", MaxToolResult)
}

// ==================== 危险命令检测 ====================

var dangerousPatterns = []string{
	"rm -rf /",
	"rm -rf /*",
	"mkfs",
	"dd if=",
	":(){:|:&};:",
	"shutdown",
	"reboot",
	"halt",
	"> /dev/sda",
	"chmod -R 777 /",
	"curl.*|.*sh",
	"wget.*|.*sh",
	"git push --force",
	"drop table",
	"drop database",
}

// IsDangerous 返回命中的危险模式；安全返回空串
func IsDangerous(command string) string {
	if command == "" {
		return ""
	}
	lo := strings.ToLower(command)
	for _, pat := range dangerousPatterns {
		if strings.Contains(lo, pat) {
			return pat
		}
	}
	return ""
}

// ==================== 参数提取 ====================

func getStr(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

func getInt(args map[string]any, key string, def int) int {
	if v, ok := args[key].(float64); ok {
		return int(v)
	}
	return def
}

// ==================== 终端命令 ====================

func toolExecuteCommand(args map[string]any) string {
	cmd := getStr(args, "command")
	if cmd == "" {
		return "✗ 缺少参数: command"
	}
	cwd := getStr(args, "cwd")
	timeout := getInt(args, "timeout", 60)
	if timeout <= 0 {
		timeout = 60
	}
	if timeout > 600 {
		timeout = 600
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	c := exec.CommandContext(ctx, "/bin/sh", "-c", cmd)
	if cwd != "" {
		c.Dir = cwd
	}
	out, err := c.CombinedOutput()

	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Sprintf("✗ 命令执行超时 (超过%d秒)", timeout)
	}
	if err != nil {
		if cwd != "" && !dirExists(cwd) {
			return fmt.Sprintf("✗ 工作目录不存在: %s", cwd)
		}
		code := -1
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		if code == -1 {
			return "✗ 命令执行失败"
		}
		return truncateResult(fmt.Sprintf("✗ 命令执行失败 (退出码: %d):\n%s", code, string(out)))
	}
	if len(out) == 0 {
		return "✓ 命令执行成功（无输出）"
	}
	return truncateResult(fmt.Sprintf("✓ 命令执行成功:\n%s", string(out)))
}

// ==================== 文件操作 ====================

func toolReadFile(args map[string]any) string {
	path := getStr(args, "file_path")
	if path == "" {
		return "✗ 缺少参数: file_path"
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Sprintf("✗ 文件不存在: %s", path)
	}
	if info.IsDir() {
		return fmt.Sprintf("✗ 路径不是文件: %s", path)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("✗ 读取文件时出错: %s", path)
	}
	text := string(content)

	ls := getInt(args, "line_start", 0)
	le := getInt(args, "line_end", 0)

	// 行数统计与 C 版一致：\n 数量 +（末行无 \n 则 +1）
	total := strings.Count(text, "\n")
	if len(text) > 0 && !strings.HasSuffix(text, "\n") {
		total++
	}

	if ls <= 0 && le <= 0 {
		return truncateResult(fmt.Sprintf("✓ 文件读取成功 (共%d行):\n%s", total, text))
	}

	start := ls
	if start < 1 {
		start = 1
	}
	end := le
	if end <= 0 || end > total {
		end = total
	}
	if start > end {
		return fmt.Sprintf("✗ 行范围无效: %d-%d，文件共 %d 行", ls, le, total)
	}

	// 逐字节扫描切片（与 C 版单遍扫描一致）
	var sb strings.Builder
	fmt.Fprintf(&sb, "✓ 文件读取成功 (第%d-%d行，共%d行):\n", start, end, total)
	lineno := 0
	segStart := 0
	for i := 0; i <= len(text); i++ {
		if i == len(text) || text[i] == '\n' {
			lineno++
			if lineno >= start && lineno <= end {
				sb.WriteString(text[segStart:i])
				sb.WriteString("\n")
			}
			if lineno > end {
				break
			}
			segStart = i + 1
		}
	}
	return truncateResult(sb.String())
}

func toolWriteFile(args map[string]any) string {
	path := getStr(args, "file_path")
	content := getStr(args, "content")
	if path == "" {
		return "✗ 缺少参数: file_path"
	}
	if getStr(args, "content") == "" && args["content"] == nil {
		return "✗ 缺少参数: content"
	}

	// 自动创建父目录
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Sprintf("✗ 写入文件时出错: %s", path)
		}
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Sprintf("✗ 写入文件时出错: %s", path)
	}
	return fmt.Sprintf("✓ 文件写入成功: %s", path)
}

func toolListDir(args map[string]any) string {
	path := getStr(args, "path")
	if path == "" {
		path = "."
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Sprintf("✗ 目录不存在: %s", path)
	}
	if !info.IsDir() {
		return fmt.Sprintf("✗ 路径不是目录: %s", path)
	}

	ents, err := os.ReadDir(path)
	if err != nil {
		return fmt.Sprintf("✗ 列出目录时出错: %s", path)
	}

	type dent struct {
		name  string
		isDir bool
	}
	items := make([]dent, 0, len(ents))
	ndirs, nfiles := 0, 0
	for _, e := range ents {
		if e.Name() == "." || e.Name() == ".." {
			continue
		}
		d := dent{name: e.Name(), isDir: e.IsDir()}
		if d.isDir {
			ndirs++
		} else {
			nfiles++
		}
		items = append(items, d)
	}
	// 目录优先，名称大小写不敏感排序
	sort.Slice(items, func(i, j int) bool {
		if items[i].isDir != items[j].isDir {
			return items[i].isDir
		}
		return strings.ToLower(items[i].name) < strings.ToLower(items[j].name)
	})

	var sb strings.Builder
	fmt.Fprintf(&sb, "✓ 目录 %s 共 %d 项（%d 个文件夹，%d 个文件）:", path, len(items), ndirs, nfiles)
	if len(items) == 0 {
		sb.WriteString("\n（空目录）")
	} else {
		for _, it := range items {
			sb.WriteString("\n")
			sb.WriteString(it.name)
			if it.isDir {
				sb.WriteString("/")
			}
		}
	}
	return sb.String()
}

func toolDeleteFile(args map[string]any) string {
	path := getStr(args, "file_path")
	if path == "" {
		return "✗ 缺少参数: file_path"
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Sprintf("✗ 路径不存在: %s", path)
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Sprintf("✗ 删除时出错: %s", path)
	}
	return fmt.Sprintf("✓ 已删除: %s", path)
}

func toolMoveFile(args map[string]any) string {
	src := getStr(args, "source")
	dst := getStr(args, "target")
	if src == "" || dst == "" {
		return "✗ 缺少参数: source/target"
	}
	if _, err := os.Stat(src); err != nil {
		return fmt.Sprintf("✗ 源路径不存在: %s", src)
	}
	if dir := filepath.Dir(dst); dir != "." && dir != "" {
		os.MkdirAll(dir, 0o755)
	}
	if err := os.Rename(src, dst); err == nil {
		return fmt.Sprintf("✓ 已移动: %s → %s", src, dst)
	}
	// 跨设备回退：复制 + 删除
	if copyPath(src, dst) == nil && os.RemoveAll(src) == nil {
		return fmt.Sprintf("✓ 已移动: %s → %s", src, dst)
	}
	return fmt.Sprintf("✗ 移动时出错: %s", src)
}

func toolCopyFile(args map[string]any) string {
	src := getStr(args, "source")
	dst := getStr(args, "target")
	if src == "" || dst == "" {
		return "✗ 缺少参数: source/target"
	}
	if _, err := os.Stat(src); err != nil {
		return fmt.Sprintf("✗ 源路径不存在: %s", src)
	}
	if err := copyPath(src, dst); err != nil {
		return fmt.Sprintf("✗ 复制时出错: %s", src)
	}
	kind := "文件"
	if fi, err := os.Stat(src); err == nil && fi.IsDir() {
		kind = "文件夹"
	}
	return fmt.Sprintf("✓ %s已复制: %s → %s", kind, src, dst)
}

// ==================== 环境信息 ====================

func toolGetCwd(args map[string]any) string {
	cwd, err := os.Getwd()
	if err != nil {
		return "✗ 获取当前目录失败"
	}
	return fmt.Sprintf("✓ 当前工作目录: %s", cwd)
}

func toolGetSystemInfo(args map[string]any) string {
	hostname, _ := os.Hostname()
	cwd, _ := os.Getwd()
	if hostname == "" {
		hostname = "unknown"
	}
	return fmt.Sprintf(
		"✓ 系统信息:\n  操作系统: %s\n  系统版本: %s\n  机器架构: %s\n  编译环境: %s\n  主机名: %s\n  当前目录: %s",
		runtime.GOOS, kernelVersion(), runtime.GOARCH, runtime.Version(), hostname, cwd)
}

func kernelVersion() string {
	data, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return runtime.GOOS
	}
	return strings.TrimSpace(string(data))
}

// ==================== 辅助 ====================

func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

func copyPath(src, dst string) error {
	fi, err := os.Stat(src)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(src, p)
			target := filepath.Join(dst, rel)
			if info.IsDir() {
				return os.MkdirAll(target, 0o755)
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			return os.WriteFile(target, data, info.Mode())
		})
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, fi.Mode())
}

// ==================== 工具注册表 ====================

var BaseTools = []ToolDef{
	{
		"execute_command",
		"执行终端命令。用于运行 shell 命令、安装软件包、执行脚本、查看进程等。注意：危险命令（如 rm -rf）会被拦截并要求确认。",
		`{"type":"object","properties":{"command":{"type":"string","description":"要执行的终端命令，例如 'ls -la'"},"cwd":{"type":"string","description":"命令执行的工作目录（可选），默认当前目录"},"timeout":{"type":"integer","description":"超时秒数（可选），默认60"}},"required":["command"]}`,
		toolExecuteCommand,
	},
	{
		"read_file",
		"读取本地文本文件的内容。支持 .txt、.json、.py、.md 等文本格式，可按行范围读取大文件。",
		`{"type":"object","properties":{"file_path":{"type":"string","description":"要读取的文件路径"},"line_start":{"type":"integer","description":"起始行号（从1开始，可选）"},"line_end":{"type":"integer","description":"结束行号（包含，可选）"}},"required":["file_path"]}`,
		toolReadFile,
	},
	{
		"write_file",
		"写入内容到本地文本文件。如果文件不存在会自动创建，目录不存在会自动创建。",
		`{"type":"object","properties":{"file_path":{"type":"string","description":"要写入的文件路径"},"content":{"type":"string","description":"要写入的文件内容"}},"required":["file_path","content"]}`,
		toolWriteFile,
	},
	{
		"list_dir",
		"列出目录中的文件和文件夹。用于查看目录结构。",
		`{"type":"object","properties":{"path":{"type":"string","description":"要查看的目录路径，默认为当前目录"}},"required":[]}`,
		toolListDir,
	},
	{
		"delete_file",
		"删除文件或文件夹（递归）。请谨慎使用，删除前确认路径正确。",
		`{"type":"object","properties":{"file_path":{"type":"string","description":"要删除的文件或文件夹路径"}},"required":["file_path"]}`,
		toolDeleteFile,
	},
	{
		"move_file",
		"移动或重命名文件/文件夹。",
		`{"type":"object","properties":{"source":{"type":"string","description":"源路径"},"target":{"type":"string","description":"目标路径"}},"required":["source","target"]}`,
		toolMoveFile,
	},
	{
		"copy_file",
		"复制文件或文件夹到新位置。",
		`{"type":"object","properties":{"source":{"type":"string","description":"源路径"},"target":{"type":"string","description":"目标路径"}},"required":["source","target"]}`,
		toolCopyFile,
	},
	{
		"get_cwd",
		"获取当前工作目录。",
		`{"type":"object","properties":{}}`,
		toolGetCwd,
	},
	{
		"get_system_info",
		"获取系统信息（操作系统、版本、架构、编译环境等）。",
		`{"type":"object","properties":{}}`,
		toolGetSystemInfo,
	},
}

// FindTool 按名称查找工具
func FindTool(name string) *ToolDef {
	for i := range BaseTools {
		if BaseTools[i].Name == name {
			return &BaseTools[i]
		}
	}
	return nil
}
