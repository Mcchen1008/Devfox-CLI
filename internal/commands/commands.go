package commands

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/Mcchen1008/devfox-go/internal/config"
	"github.com/Mcchen1008/devfox-go/internal/history"
	"github.com/Mcchen1008/devfox-go/internal/skills"
	"github.com/Mcchen1008/devfox-go/internal/ui"
)

func IsCommand(line string) bool {
	return strings.HasPrefix(line, "/")
}

// ==================== @文件引用 ====================

// ExpandMentions 将 @文件路径 替换为文件内容（对应 C 版 cmd_expand_mentions）
func ExpandMentions(text string) (string, []string) {
	cwd, _ := os.Getwd()
	var sb strings.Builder
	loaded := []string{}
	p := 0
	for p < len(text) {
		if text[p] == '@' {
			start := p
			p++
			quoted := false
			if p < len(text) && text[p] == '"' {
				quoted = true
				p++
			}
			var pathBuf strings.Builder
			for p < len(text) {
				ch := text[p]
				if quoted && ch == '"' {
					break
				}
				if !quoted && (ch == ' ' || ch == '\t' || ch == '\n' || ch == '@' || ch == '"') {
					break
				}
				pathBuf.WriteByte(ch)
				p++
			}
			if quoted && p < len(text) && text[p] == '"' {
				p++
			}
			rel := pathBuf.String()
			handled := false
			if rel != "" {
				resolved := rel
				if !filepath.IsAbs(rel) {
					resolved = filepath.Join(cwd, rel)
				}
				if fi, err := os.Stat(resolved); err == nil && !fi.IsDir() {
					if data, err := os.ReadFile(resolved); err == nil {
						fmt.Fprintf(&sb, "\n[引用文件: %s]\n```\n%s\n```\n", rel, string(data))
						loaded = append(loaded, resolved)
						handled = true
					}
				}
			}
			if !handled {
				// 不是可读文件，保留原文（可能是 @人名 等）
				sb.WriteString(text[start:p])
			}
		} else {
			sb.WriteByte(text[p])
			p++
		}
	}
	return sb.String(), loaded
}

// ==================== 交互式添加技能 ====================

func readlineTrimmed(prompt string) string {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return ""
	}
	return strings.TrimRight(line, "\r\n")
}

func validSkillName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if i == 0 && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

func addSkillInteractive(mgr *skills.Manager) string {
	fmt.Println()
	name := readlineTrimmed("  技能名称 (英文/数字/连字符): ")
	if name == "" {
		return "已取消"
	}
	if !validSkillName(name) {
		return fmt.Sprintf("技能名称不合法: %s（仅支持英文/数字/连字符）", name)
	}
	if mgr.Get(name) != nil {
		return fmt.Sprintf("技能 [%s] 已存在，无需重复添加", name)
	}

	desc := readlineTrimmed("  技能描述 (一句话说明何时使用，决定 AI 何时自动调用): ")
	if desc == "" {
		return "描述不能为空，已取消"
	}

	fmt.Println("  请输入技能指令内容（单独一行输入 END 结束）:")
	var body strings.Builder
	for {
		line := readlineTrimmed("  > ")
		if line == "" && body.Len() == 0 {
			// 允许空行，但需要 END 结束
		}
		if line == "END" {
			break
		}
		body.WriteString(line)
		body.WriteString("\n")
	}
	if body.Len() == 0 {
		return "技能内容为空，已取消"
	}

	dir := filepath.Join(mgr.Dir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "✗ 技能目录创建失败"
	}
	path := filepath.Join(dir, "SKILL.md")
	sk := fmt.Sprintf("---\nname: %s\ndescription: %s\nuser-invocable: true\n---\n\n%s",
		name, desc, body.String())
	if err := os.WriteFile(path, []byte(sk), 0o644); err != nil {
		return "✗ 技能写入失败"
	}
	mgr.Refresh()
	return "技能已添加并立即生效，使用 /skills 可查看"
}

// ==================== 命令表驱动 ====================

type CmdFn func(mgr *skills.Manager, hist *history.History, cfg *config.Config, shouldExit *bool, message *string) bool

type CmdDef struct {
	Name string
	Desc string
	Fn   CmdFn
}

// CmdTable 在 init() 中初始化，避免与 cmdHelp 的包级初始化循环
var CmdTable []CmdDef

func init() {
	CmdTable = []CmdDef{
		{"/help", "显示帮助", cmdHelp},
		{"/exit", "退出程序", cmdExit},
		{"/quit", "退出程序", cmdExit}, // /exit 别名
		{"/clear", "清空对话记忆", cmdClear},
		{"/skills", "列出已加载的技能", cmdSkills},
		{"/addskill", "交互式添加新技能", cmdAddSkill},
		{"/time", "显示当前真实时间", cmdTime},
		{"/status", "显示运行状态", cmdStatus},
		{"/history", "查看对话历史", cmdHistory},
	}
}

func cmdExit(mgr *skills.Manager, hist *history.History, cfg *config.Config, shouldExit *bool, message *string) bool {
	*shouldExit = true
	return true
}

func cmdHelp(mgr *skills.Manager, hist *history.History, cfg *config.Config, shouldExit *bool, message *string) bool {
	var sb strings.Builder
	ui.Append(&sb, ui.Header, "可用命令")
	sb.WriteString("\n")
	for _, c := range CmdTable {
		if c.Name == "/quit" {
			continue // 别名去重
		}
		sb.WriteString("  ")
		ui.Append(&sb, ui.Cyan, c.Name)
		sb.WriteString("   ")
		ui.Append(&sb, ui.Gray, c.Desc)
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	ui.Append(&sb, ui.Header, "@ 文件引用")
	sb.WriteString("\n  提问中使用 @文件路径 自动读取文件内容附加到消息：\n  ")
	ui.Append(&sb, ui.Prompt, "你 > ")
	sb.WriteString("帮我看看 @agent.go 里的工具调用逻辑\n  ")
	ui.Append(&sb, ui.Prompt, "你 > ")
	sb.WriteString("对比 @main.go 和 @tools.go 的差异\n\n")
	ui.Append(&sb, ui.Header, "快捷键")
	sb.WriteString("\n  Ctrl+C 中断当前任务 · 再次 Ctrl+C 退出")
	*message = sb.String()
	return true
}

func cmdClear(mgr *skills.Manager, hist *history.History, cfg *config.Config, shouldExit *bool, message *string) bool {
	hist.Clear()
	*message = "对话记忆已清空"
	return true
}

func cmdSkills(mgr *skills.Manager, hist *history.History, cfg *config.Config, shouldExit *bool, message *string) bool {
	*message = mgr.ListText()
	return true
}

func cmdAddSkill(mgr *skills.Manager, hist *history.History, cfg *config.Config, shouldExit *bool, message *string) bool {
	*message = addSkillInteractive(mgr)
	return true
}

func cmdTime(mgr *skills.Manager, hist *history.History, cfg *config.Config, shouldExit *bool, message *string) bool {
	*message = NowCN()
	return true
}

func cmdStatus(mgr *skills.Manager, hist *history.History, cfg *config.Config, shouldExit *bool, message *string) bool {
	var sb strings.Builder
	sb.WriteString("运行状态:\n")
	if cfg != nil {
		sb.WriteString("  模型: ")
		ui.Append(&sb, ui.Cyan, cfg.ModelName)
		sb.WriteString("\n  API: ")
		ui.Append(&sb, ui.Gray, cfg.BaseURL)
		sb.WriteString("\n  Thinking: ")
		if cfg.EnableThinking {
			ui.Append(&sb, ui.OK, "开启")
		} else {
			ui.Append(&sb, ui.Gray, "关闭")
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n  技能数: ")
	ui.Append(&sb, ui.Cyan, fmt.Sprintf("%d", len(mgr.Items)))
	sb.WriteString("\n  记忆轮数: ")
	ui.Append(&sb, ui.Cyan, fmt.Sprintf("%d", hist.MaxUserTurns))
	sb.WriteString("\n  工作目录: ")
	cwd, _ := os.Getwd()
	ui.Append(&sb, ui.Gray, cwd)
	sb.WriteString("\n  颜色输出: ")
	if ui.Enabled {
		ui.Append(&sb, ui.OK, "开启")
	} else {
		ui.Append(&sb, ui.Gray, "关闭")
	}
	*message = sb.String()
	return true
}

func cmdHistory(mgr *skills.Manager, hist *history.History, cfg *config.Config, shouldExit *bool, message *string) bool {
	msgs := hist.Get()
	if hist.UserCount() == 0 {
		*message = "当前没有对话历史"
		return true
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "对话历史（共 %d 轮）:\n", hist.UserCount())
	idx := 1
	for i := range msgs {
		if !msgs[i].IsUser {
			continue
		}
		sb.WriteString("  ")
		ui.Append(&sb, ui.Cyan, fmt.Sprintf("%d", idx))
		sb.WriteString(". ")
		content := msgs[i].Content
		brief := content
		if len(brief) > 60 {
			brief = brief[:60]
		}
		ui.Append(&sb, ui.Gray, brief)
		if len(content) > 60 {
			sb.WriteString("...")
		}
		sb.WriteString("\n")
		idx++
	}
	*message = sb.String()
	return true
}

// Handle 统一入口：查表分发。返回是否已处理。
func Handle(line string, mgr *skills.Manager, hist *history.History, cfg *config.Config) (handled, shouldExit bool, message string) {
	tok := strings.Fields(line)
	if len(tok) == 0 {
		return false, false, ""
	}
	for _, c := range CmdTable {
		if tok[0] == c.Name {
			handled := false
			msg := ""
			rc := c.Fn(mgr, hist, cfg, &handled, &msg)
			return rc, handled, msg
		}
	}
	return false, false, ""
}

// ==================== 中文时间 ====================

var weekdayCN = [...]string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}

func timeZoneName() string {
	if tz := os.Getenv("TZ"); tz != "" {
		return tz
	}
	if data, err := os.ReadFile("/etc/timezone"); err == nil {
		if s := strings.TrimSpace(string(data)); s != "" {
			return s
		}
	}
	_, offset := time.Now().Zone()
	return fmt.Sprintf("UTC%+d", offset/3600)
}

func NowCN() string {
	now := time.Now()
	return fmt.Sprintf("%d年%d月%d日 %s %s",
		now.Year(), int(now.Month()), now.Day(),
		weekdayCN[int(now.Weekday())], now.Format("15:04:05"))
}

func ContextCN() string {
	now := time.Now()
	return fmt.Sprintf("【当前时间】%d年%d月%d日 %s %s (时区: %s)",
		now.Year(), int(now.Month()), now.Day(),
		weekdayCN[int(now.Weekday())], now.Format("15:04:05"), timeZoneName())
}
