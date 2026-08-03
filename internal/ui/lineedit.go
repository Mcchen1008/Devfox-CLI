package ui

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"
	"unicode/utf8"
	"unsafe"
)

// Suggestion 补全候选条目
type Suggestion struct {
	Label string // 左侧显示文本
	Value string // 回车后使用的值
	Desc  string // 右侧描述（可选）
}

// Completer 根据当前输入返回补全建议；active=false 表示不显示补全
type Completer func(input string) (items []Suggestion, active bool)

var restoreRaw func()

func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// ---------- 终端宽度 ----------

func termWidth() int {
	var ws struct{ Row, Col, Xpixel, Ypixel uint16 }
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, os.Stdout.Fd(),
		syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&ws)))
	if errno != 0 || ws.Col == 0 {
		return 80
	}
	return int(ws.Col)
}

// ---------- 显示宽度（CJK 宽字符占 2 列） ----------

func isCJK(r rune) bool {
	return (r >= 0x1100 && r <= 0x115F) ||
		(r >= 0x2E80 && r <= 0xA4CF) ||
		(r >= 0xAC00 && r <= 0xD7A3) ||
		(r >= 0xF900 && r <= 0xFAFF) ||
		(r >= 0xFE30 && r <= 0xFE4F) ||
		(r >= 0xFF00 && r <= 0xFF60) ||
		(r >= 0xFFE0 && r <= 0xFFE6) ||
		(r >= 0x20000 && r <= 0x2FFFD) ||
		(r >= 0x30000 && r <= 0x3FFFD)
}

func displayWidth(rs []rune) int {
	w := 0
	for _, r := range rs {
		if r < 0x80 {
			w++
		} else if isCJK(r) {
			w += 2
		} else {
			w++
		}
	}
	return w
}

// ---------- raw 模式（逐键读取，支持方向键） ----------

type termios struct {
	Iflag  uint32
	Oflag  uint32
	Cflag  uint32
	Lflag  uint32
	Line   uint8
	Cc     [19]uint8
	Ispeed uint32
	Ospeed uint32
}

func enableRaw() {
	if !isTTY(os.Stdin) {
		return
	}
	var old termios
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, os.Stdin.Fd(),
		syscall.TCGETS, uintptr(unsafe.Pointer(&old))); errno != 0 {
		return
	}
	raw := old
	raw.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK |
		syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	raw.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	raw.Cflag &^= syscall.CSIZE | syscall.PARENB
	raw.Cflag |= syscall.CS8
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	syscall.Syscall(syscall.SYS_IOCTL, os.Stdin.Fd(),
		syscall.TCSETS, uintptr(unsafe.Pointer(&raw)))
	restoreRaw = func() {
		syscall.Syscall(syscall.SYS_IOCTL, os.Stdin.Fd(),
			syscall.TCSETS, uintptr(unsafe.Pointer(&old)))
	}
}

func disableRaw() {
	if restoreRaw != nil {
		restoreRaw()
		restoreRaw = nil
	}
}

// ---------- UTF-8 读取 ----------

func readRune(r *bufio.Reader) (rune, error) {
	b, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	if b < 0x80 {
		return rune(b), nil
	}
	n := 0
	switch {
	case b&0xE0 == 0xC0:
		n = 1
	case b&0xF0 == 0xE0:
		n = 2
	case b&0xF8 == 0xF0:
		n = 3
	default:
		return rune(b), nil
	}
	buf := make([]byte, n+1)
	buf[0] = b
	for i := 0; i < n; i++ {
		bb, err := r.ReadByte()
		if err != nil {
			return rune(b), nil
		}
		buf[i+1] = bb
	}
	ru, _ := utf8.DecodeRune(buf)
	return ru, nil
}

// 共享 stdin 读取器：降级模式复用，避免 bufio 预读缓冲丢失
var stdinReader *bufio.Reader

func getStdinReader() *bufio.Reader {
	if stdinReader == nil {
		stdinReader = bufio.NewReader(os.Stdin)
	}
	return stdinReader
}

// ---------- 主入口 ----------

// ReadLine 交互式行输入：上下边框（宽度自适应）、❯ 提示符、
// @ 文件补全、/ 命令补全，上下键选择，回车确认。
// 支持 ← → 光标移动、Home/End、退格/Delete 编辑。
// 非 TTY 环境自动降级为普通行读取。
// 返回 (输入内容, 是否被 Ctrl+C 中断)。
func ReadLine(prompt string, complete Completer) (string, bool) {
	// 降级模式：管道/重定向时逐行读取
	if !isTTY(os.Stdin) || !isTTY(os.Stdout) {
		line, _ := getStdinReader().ReadString('\n')
		return strings.TrimRight(line, "\r\n"), false
	}

	enableRaw()
	defer disableRaw()

	w := termWidth()
	if w < 10 {
		w = 10
	}
	border := strings.Repeat("─", w)

	fmt.Println(border)
	fmt.Print(prompt)

	reader := bufio.NewReader(os.Stdin)
	inputRunes := []rune{}
	cursor := 0 // 光标位置（rune 索引）
	sel := -1
	suppress := false // @ 选择后抑制立即再弹列表

	var items []Suggestion
	active := false

	completeAt := func(in string) ([]Suggestion, bool) {
		if suppress {
			return nil, false
		}
		if complete == nil {
			return nil, false
		}
		return complete(in)
	}

	redraw := func() {
		input := string(inputRunes)
		// 清输入行并重画
		fmt.Print("\r\033[K")
		fmt.Print(prompt + input)
		// 换到边框行并重画下边框
		fmt.Print("\n\r\033[K")
		fmt.Print(border)
		// 补全列表
		items, active = completeAt(input)
		shown := 0
		if active && len(items) > 0 {
			maxShow := 8
			start := 0
			if sel >= maxShow {
				start = sel - maxShow + 1
			}
			end := start + maxShow
			if end > len(items) {
				end = len(items)
			}
			for i := start; i < end; i++ {
				shown++
				it := items[i]
				fmt.Print("\n\r\033[K")
				if i == sel {
					fmt.Print("\033[7m " + it.Label)
					if it.Desc != "" {
						fmt.Print("  " + it.Desc)
					}
					fmt.Print(" \033[0m")
				} else {
					fmt.Print(" " + it.Label)
					if it.Desc != "" {
						fmt.Print("  " + it.Desc)
					}
				}
			}
		}
		fmt.Print("\033[J") // 清掉残留
		// 光标回到输入行（上移 下边框1行 + 列表shown行）
		if shown > 0 {
			fmt.Printf("\r\033[%dA", shown+1)
		} else {
			fmt.Printf("\r\033[1A")
		}
		// 光标定位：后退到 cursor 位置（光标后字符的显示宽度）
		if cursor < len(inputRunes) {
			back := displayWidth(inputRunes[cursor:])
			if back > 0 {
				fmt.Printf("\033[%dD", back)
			}
		}
	}

	for {
		redraw()
		ru, err := readRune(reader)
		if err != nil {
			return string(inputRunes), false
		}

		switch ru {
		case '\r', '\n': // Enter
			input := string(inputRunes)
			if active && sel >= 0 && len(items) > 0 {
				if strings.HasPrefix(input, "@") {
					// @ 模式：填入选中项，继续编辑
					inputRunes = []rune("@" + items[sel].Value)
					cursor = len(inputRunes)
					suppress = true
					sel = -1
					continue
				}
				if strings.HasPrefix(input, "/") {
					// / 模式：填入选中命令并提交
					inputRunes = []rune(items[sel].Value)
					input = string(inputRunes)
				}
			}
			// 结束输入：清掉边框与列表
			fmt.Print("\n\r\033[K\033[J")
			return input, false

		case 0x7f, 0x08: // Backspace：删除光标前字符
			if cursor > 0 {
				inputRunes = append(inputRunes[:cursor-1], inputRunes[cursor:]...)
				cursor--
				suppress = false
				sel = -1
			}

		case 0x03: // Ctrl+C
			fmt.Print("^C\n")
			fmt.Print("\r\033[K\033[J")
			return "", true

		case 0x1b: // ESC 序列
			b2, err := reader.ReadByte()
			if err != nil {
				continue
			}
			if b2 != '[' {
				continue
			}
			b3, err := reader.ReadByte()
			if err != nil {
				continue
			}
			switch b3 {
			case 'A': // ↑
				if active && len(items) > 0 {
					if sel < 0 {
						sel = 0
					} else if sel > 0 {
						sel--
					}
				}
			case 'B': // ↓
				if active && len(items) > 0 {
					if sel < 0 {
						sel = 0
					} else if sel < len(items)-1 {
						sel++
					}
				}
			case 'C': // → 光标右移
				if cursor < len(inputRunes) {
					cursor++
				}
			case 'D': // ← 光标左移
				if cursor > 0 {
					cursor--
				}
			case 'H': // Home
				cursor = 0
			case 'F': // End
				cursor = len(inputRunes)
			case '1', '3', '4', '7', '8': // 带 ~ 的扩展键
				_, _ = reader.ReadByte() // 吃掉 '~'
				switch b3 {
				case '1': // Home
					cursor = 0
				case '3': // Delete：删除光标处字符
					if cursor < len(inputRunes) {
						inputRunes = append(inputRunes[:cursor], inputRunes[cursor+1:]...)
						suppress = false
						sel = -1
					}
				case '4': // End
					cursor = len(inputRunes)
				}
			}

		default:
			if ru >= 0x20 {
				// 在光标位置插入
				inputRunes = append(inputRunes, 0)
				copy(inputRunes[cursor+1:], inputRunes[cursor:])
				inputRunes[cursor] = ru
				cursor++
				suppress = false
				sel = -1
			}
		}
	}
}
