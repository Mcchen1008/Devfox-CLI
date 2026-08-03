package ui

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	RS  = "\033[0m"
	BLD = "\033[1m"
	DIM = "\033[2m"
	ITA = "\033[3m"
)

type Style int

const (
	Plain Style = iota
	Bold
	Dim
	Italic
	Underline
	Red
	Green
	Yellow
	Blue
	Magenta
	Cyan
	White
	Gray
	BrightCyan
	BrightGreen
	BrightBlue
	BrightYellow
	BrightRed
	Header
	Prompt
	AgentReply
	ToolCall
	ToolResult
	Thinking
	OK
	Error
	Warn
	Info
)

var styles = map[Style]string{
	Plain:       "",
	Bold:        BLD,
	Dim:         DIM,
	Italic:      ITA,
	Underline:   "\033[4m",
	Red:         "\033[31m",
	Green:       "\033[32m",
	Yellow:      "\033[33m",
	Blue:        "\033[34m",
	Magenta:     "\033[35m",
	Cyan:        "\033[36m",
	White:       "\033[37m",
	Gray:        "\033[90m",
	BrightCyan:  "\033[96m",
	BrightGreen: "\033[92m",
	BrightBlue:  "\033[94m",
	BrightYellow:"\033[93m",
	BrightRed:   "\033[91m",
	Header:      BLD + "\033[96m",
	Prompt:      BLD + "\033[36m",
	AgentReply:  BLD + "\033[92m",
	ToolCall:    BLD + "\033[94m",
	ToolResult:  "\033[90m",
	Thinking:    DIM + ITA,
	OK:          "\033[32m",
	Error:       "\033[31m",
	Warn:        "\033[33m",
	Info:        "\033[36m",
}

// Enabled 是否启用 ANSI 颜色
var Enabled bool

func Init() {
	if os.Getenv("NO_COLOR") != "" {
		Enabled = false
		return
	}
	if os.Getenv("FORCE_COLOR") != "" {
		Enabled = true
		return
	}
	if os.Getenv("TERM") == "dumb" {
		Enabled = false
		return
	}
	fi, err := os.Stdout.Stat()
	Enabled = err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func style(s Style) string {
	if !Enabled {
		return ""
	}
	return styles[s]
}

func Reset() string {
	if !Enabled {
		return ""
	}
	return RS
}

func Puts(s Style, text string) {
	fmt.Print(style(s) + text + Reset())
}

// Append 将带样式文本写入 strings.Builder（用于 /help 等命令输出）
func Append(sb *strings.Builder, s Style, text string) {
	if Enabled {
		sb.WriteString(styles[s])
		sb.WriteString(text)
		sb.WriteString(RS)
	} else {
		sb.WriteString(text)
	}
}

func Rule() {
	Puts(Dim, "────────────────────────────────────────────────────────────")
	fmt.Println()
}

var banner = []string{
	"██████╗ ███████╗██╗   ██╗███████╗ ██████╗ ██╗  ██╗",
	"██╔══██╗██╔════╝██║   ██║██╔════╝██╔═══██╗╚██╗██╔╝",
	"██║  ██║█████╗  ██║   ██║█████╗  ██║   ██║ ╚███╔╝ ",
	"██║  ██║██╔══╝  ╚██╗ ██╔╝██╔══╝  ██║   ██║ ██╔██╗ ",
	"██████╔╝███████╗ ╚████╔╝ ██║     ╚██████╔╝██╔╝ ██╗",
	"╚═════╝ ╚══════╝  ╚═══╝  ╚═╝      ╚═════╝ ╚═╝  ╚═╝",
}

func Banner(name, release, version, tagline string) {
	if !Enabled {
		fmt.Printf("%s %s v%s - %s\n", name, release, version, tagline)
		return
	}
	fmt.Println()
	for _, line := range banner {
		Puts(BrightCyan, line)
		fmt.Println()
	}
	Puts(Bold, "  "+name+" "+release+" v"+version)
	fmt.Println()
	Puts(Gray, "  "+tagline)
	fmt.Println()
}

// Spinner 思考动画
type Spinner struct {
	mu    sync.Mutex
	stop  chan struct{}
	done  chan struct{}
	msg   string
}

func SpinnerStart(msg string) *Spinner {
	s := &Spinner{stop: make(chan struct{}), done: make(chan struct{}), msg: msg}
	if !Enabled {
		close(s.done)
		return s
	}
	go func() {
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		i := 0
		for {
			select {
			case <-s.stop:
				close(s.done)
				return
			default:
			}
			fmt.Printf("\r%s %s %s", styles[Cyan], frames[i%len(frames)], s.msg)
			i++
			time.Sleep(100 * time.Millisecond)
		}
	}()
	return s
}

func (s *Spinner) Stop(final string) {
	if !Enabled {
		return
	}
	close(s.stop)
	<-s.done
	fmt.Printf("\r%s %s %s%s\n", styles[Cyan], "✓", final, Reset())
}
