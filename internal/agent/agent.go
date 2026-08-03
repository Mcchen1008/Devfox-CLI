package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Mcchen1008/devfox-go/internal/commands"
	"github.com/Mcchen1008/devfox-go/internal/config"
	"github.com/Mcchen1008/devfox-go/internal/history"
	"github.com/Mcchen1008/devfox-go/internal/llm"
	"github.com/Mcchen1008/devfox-go/internal/skills"
	"github.com/Mcchen1008/devfox-go/internal/tools"
	"github.com/Mcchen1008/devfox-go/internal/ui"
)

const (
	MaxHistoryTurns  = 10
	ConfirmDangerous = true
)

// ---------- 系统提示词 ----------

const promptHead = `你是一个具有终端命令执行和文件读写能力的 AI Agent，运行在用户的电脑上。

## 时间感知

你能够感知当前的真实日期和时间。每次用户发言前，系统都会向你注入一条【当前时间】消息，内容格式如：
【当前时间】2026年8月2日 星期日 12:56:30 (时区: Asia/Shanghai)

请务必注意：
- 涉及"今天、昨天、明天、几号、星期几、几点"等问题时，以注入的【当前时间】为准回答
- 需要生成带日期/时间的文件、日志、备份名时，使用【当前时间】中的真实时间
- 不要臆造或猜测时间

## 可用工具

1. execute_command - 执行终端命令（shell 命令、安装软件包、运行脚本等）
2. read_file - 读取本地文本文件（支持按行范围读取大文件）
3. write_file - 写入内容到本地文本文件（自动创建目录）
4. list_dir - 列出目录内容
5. delete_file - 删除文件或文件夹（谨慎使用）
6. move_file - 移动或重命名文件/文件夹
7. copy_file - 复制文件或文件夹
8. get_cwd - 获取当前工作目录
9. get_system_info - 获取系统信息
10. use_skill - 加载并使用技能（技能是预定义的专业指令集）

## 可用技能

以下技能已自动从 skills/ 目录检测到，当用户的任务匹配技能描述时，调用 use_skill 加载对应技能：

`

const promptTail = `

## 工作原则

1. 先理解用户意图，再决定需要哪些工具
2. 复杂任务拆解为多个工具调用，一步步完成
3. 文件操作前先确认路径（可用 list_dir 查看）
4. 命令执行后根据输出判断结果，必要时继续调用工具
5. 删除、覆盖等破坏性操作前要再三确认路径正确
6. 不要执行明显危险的操作（如格式化磁盘、删除系统目录）
7. 任务匹配技能描述时，优先调用 use_skill 加载技能，再按技能指示执行
8. 完成任务后，用中文清晰总结做了什么、结果如何

## 注意事项

- 优先使用工具获取真实信息，不要臆测
- 如果工具返回错误，分析原因并尝试修正
- 遇到不确定的情况，如实告知用户
- 如果用户输入无法理解或找不到对应命令/文件/工具，**直接向用户提问澄清意图**，不要反复尝试搜索或执行无关命令（最多尝试 1-2 次即可）
- 始终使用中文回复用户
`

func buildSystemPrompt(mgr *skills.Manager) string {
	return promptHead + mgr.Summary() + promptTail
}

// ---------- 危险命令确认 ----------

func confirmDangerousCmd(cmd string) bool {
	pat := tools.IsDangerous(cmd)
	if pat == "" {
		return true
	}
	fmt.Println()
	ui.Puts(ui.Warn, "[警告]")
	fmt.Println()
	fmt.Print(" 检测到危险命令模式: ")
	ui.Puts(ui.Bold, pat)
	fmt.Println()
	fmt.Print("确认执行吗？(yes/no): ")
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	line = strings.TrimSpace(line)
	switch line {
	case "yes", "y", "是", "确认", "确定":
		return true
	}
	return false
}

// ---------- 单工具执行 ----------

func runTool(hist *history.History, mgr *skills.Manager, id, name string, args map[string]any) {
	fmt.Println()
	ui.Puts(ui.ToolCall, "> 调用工具")
	fmt.Print(": ")
	ui.Puts(ui.Bold, name)
	fmt.Println()
	for k, v := range args {
		disp, _ := json.Marshal(v)
		s := string(disp)
		if len(s) > 100 {
			s = s[:100] + "..."
		}
		fmt.Print("    ")
		ui.Puts(ui.Gray, k)
		fmt.Print(": ")
		ui.Puts(ui.White, s)
		fmt.Println()
	}

	var result string
	if td := tools.FindTool(name); td != nil {
		if name == "execute_command" && ConfirmDangerous {
			cmd, _ := args["command"].(string)
			if cmd != "" && !confirmDangerousCmd(cmd) {
				result = "✗ 用户取消了危险命令执行"
			} else {
				result = td.Fn(args)
			}
		} else {
			result = td.Fn(args)
		}
	} else if name == "use_skill" {
		sn, _ := args["skill_name"].(string)
		result = mgr.Use(sn)
	} else {
		result = fmt.Sprintf("✗ 未知工具: %s", name)
	}

	rl := len(result)
	brief := result
	if rl > 200 {
		brief = result[:200]
	}
	fmt.Print("  ")
	ui.Puts(ui.ToolResult, "<- 结果")
	fmt.Print(": ")
	st := ui.Plain
	switch {
	case strings.HasPrefix(result, "✓"):
		st = ui.OK
	case strings.HasPrefix(result, "✗"):
		st = ui.Error
	case strings.HasPrefix(result, "[警告]"):
		st = ui.Warn
	}
	ui.Puts(st, brief)
	if rl > 200 {
		fmt.Print("...")
	}
	fmt.Println()

	hist.Add("tool", result, id, "")
}

// ---------- 主循环 ----------

func Run(cfg *config.Config, mgr *skills.Manager, limits *config.Limits) int {
	ui.Banner("Devfox", "beta", "0.1", "终端 AI 助手 · 命令执行 / 文件操作 / 技能系统")
	fmt.Println()

	if len(mgr.Items) > 0 {
		ui.Puts(ui.Info, "[技能]")
		fmt.Printf(" 已检测到 ")
		ui.Puts(ui.Bold, fmt.Sprintf("%d", len(mgr.Items)))
		fmt.Printf(" 个技能: ")
		ui.Puts(ui.Cyan, mgr.Names())
		fmt.Println()
	} else {
		ui.Puts(ui.Info, "[技能]")
		fmt.Println(" 未检测到技能（可在 skills/ 目录添加 SKILL.md）")
	}
	ui.Puts(ui.Info, "[命令]")
	fmt.Println(" 输入 /help 查看所有命令 · @文件 可引用文件")
	ui.Rule()

	hist := history.New(MaxHistoryTurns)

	// Ctrl+C 优雅退出
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGINT)
	stopSpinner := make(chan struct{})
	go func() {
		<-sigCh
		fmt.Println()
		ui.Puts(ui.AgentReply, "Agent > ")
		fmt.Println("再见！")
		os.Exit(0)
	}()
	_ = stopSpinner

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	for {
		fmt.Println()
		ui.Puts(ui.Prompt, "你 > ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimRight(scanner.Text(), "\r\n")
		if input == "" {
			continue
		}

		if input == "exit" || input == "quit" || input == "退出" {
			fmt.Println()
			ui.Puts(ui.AgentReply, "Agent > ")
			fmt.Println("再见！")
			break
		}

		if commands.IsCommand(input) {
			handled, shouldExit, msg := commands.Handle(input, mgr, hist, cfg)
			if handled {
				if shouldExit {
					fmt.Println()
					ui.Puts(ui.AgentReply, "Agent > ")
					fmt.Println("再见！")
					break
				}
				if msg != "" {
					fmt.Printf("\n%s\n", msg)
				}
				continue
			}
		}

		expanded, loaded := commands.ExpandMentions(input)
		if len(loaded) > 0 {
			ui.Puts(ui.Info, "[文件]")
			fmt.Printf(" 已加载 %d 个文件: ", len(loaded))
			for i, f := range loaded {
				if i > 0 {
					fmt.Print(", ")
				}
				ui.Puts(ui.Cyan, f)
			}
			fmt.Println()
		}
		input = expanded

		tctx := commands.ContextCN()
		fmt.Println()
		ui.Puts(ui.Warn, "[时间]")
		fmt.Printf(" %s\n", tctx)

		hist.Add("user", input, "", "")
		hist.Trim()

		fmt.Println()
		ui.Puts(ui.Dim, "正在思考...")
		fmt.Println()

		sys := buildSystemPrompt(mgr)
		finalReply := ""
		done := false

		totalCalls := 0   // 总体工具调用计数
		sameKey := ""     // 上次工具调用 key（名称+参数）
		sameCount := 0    // 相同工具连续调用计数

		for round := 0; round < limits.MaxToolRounds; round++ {
			payload, err := llm.BuildPayload(cfg, mgr, hist, sys, tctx, input)
			if err != nil {
				finalReply = "✗ 请求体构建失败"
				done = true
				break
			}

			spin := ui.SpinnerStart("思考中")
			body, err := llm.Chat(cfg, payload)
			spin.Stop("✓ 完成！")
			if err != nil {
				fmt.Println()
				ui.Puts(ui.Error, "[错误]")
				fmt.Printf(" API 错误: %v\n", err)
				fmt.Println("    提示: 检查网络连接和 API Key 配置")
				finalReply = "请求失败，请重试。"
				done = true
				break
			}

			var root struct {
				Choices []struct {
					Message struct {
						Content          string          `json:"content"`
						ReasoningContent string          `json:"reasoning_content"`
						Reasoning        string          `json:"reasoning"`
						ToolCalls        json.RawMessage `json:"tool_calls"`
					} `json:"message"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(body), &root); err != nil {
				fmt.Println()
				ui.Puts(ui.Error, "[错误]")
				fmt.Printf(" 响应解析失败: %v\n", err)
				finalReply = "响应解析失败。"
				done = true
				break
			}
			if len(root.Choices) == 0 {
				finalReply = "响应缺少 choices。"
				done = true
				break
			}
			m := root.Choices[0].Message

			reasoning := m.ReasoningContent
			if reasoning == "" {
				reasoning = m.Reasoning
			}
			if reasoning != "" {
				brief := reasoning
				if len(brief) > 300 {
					brief = brief[:300]
				}
				fmt.Println()
				ui.Puts(ui.Thinking, "思考:")
				fmt.Print(" ")
				ui.Puts(ui.Thinking, brief)
				if len(reasoning) > 300 {
					fmt.Print("...")
				}
				fmt.Println()
			}

			// 保存 assistant 原始消息（含 tool_calls）用于回传
			rawMsg, _ := json.Marshal(m)
			hist.Add("assistant", m.Content, "", string(rawMsg))

			var toolCalls []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			}
			hasTools := len(m.ToolCalls) > 0 && string(m.ToolCalls) != "null"
			if hasTools {
				if err := json.Unmarshal(m.ToolCalls, &toolCalls); err != nil {
					hasTools = false
				}
			}

			if hasTools && len(toolCalls) > 0 {
				fmt.Println()
				ui.Puts(ui.ToolCall, "[工具]")
				fmt.Printf(" 第 %d 轮调用\n", round+1)
				for _, tc := range toolCalls {
					id := tc.ID
					if id == "" {
						id = "call_c"
					}
					name := tc.Function.Name
					if name == "" {
						hist.Add("tool", "✗ 工具调用缺少函数名", id, "")
						continue
					}
					var args map[string]any
					if tc.Function.Arguments != "" {
						if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
							args = nil
						}
					}

					// ---- 工具调用次数限制（可配置） ----
					argsKey := ""
					if args != nil {
						if b, err := json.Marshal(args); err == nil {
							argsKey = string(b)
						}
					}
					totalCalls++
					if totalCalls > limits.MaxToolCallsTotal {
						finalReply = fmt.Sprintf("✗ 工具调用总次数超过上限（%d 次），已停止。请尝试拆分任务。", limits.MaxToolCallsTotal)
						done = true
						break
					}
					key := name + "\x00" + argsKey
					if key == sameKey {
						sameCount++
					} else {
						sameKey = key
						sameCount = 1
					}
					if sameCount > limits.MaxSameToolCalls {
						finalReply = fmt.Sprintf("✗ 连续 %d 次调用相同工具（%s，参数相同）仍未解决问题，已停止。请尝试其他思路。", limits.MaxSameToolCalls, name)
						done = true
						break
					}

					runTool(hist, mgr, id, name, args)
				}
				if done {
					break
				}
				fmt.Println()
				ui.Puts(ui.Dim, "继续处理...")
				fmt.Println()
				continue
			}

			if m.Content != "" {
				finalReply = m.Content
			} else if reasoning != "" {
				// content 为空时用思考内容兜底，避免空白回复
				finalReply = "（模型未生成文字回复，以下是其思考过程）\n" + reasoning
			} else {
				finalReply = "（任务已完成，无文字回复）"
			}
			done = true
			break
		}

		if !done {
			finalReply = fmt.Sprintf("✗ 工具调用轮数超过上限（%d 轮），已停止。请尝试拆分任务。", limits.MaxToolRounds)
		}
		fmt.Println()
		ui.Puts(ui.AgentReply, "Agent > ")
		fmt.Printf("%s\n", finalReply)
	}

	return 0
}
