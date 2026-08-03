package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Mcchen1008/devfox-go/internal/agent"
	"github.com/Mcchen1008/devfox-go/internal/config"
	"github.com/Mcchen1008/devfox-go/internal/skills"
	"github.com/Mcchen1008/devfox-go/internal/ui"
)

const (
	Name    = "Devfox"
	Version = "0.1"
	Release = "beta"
	Tagline = "终端 AI 助手 · 命令执行 / 文件操作 / 技能系统"
	UA      = "devfox-go/" + Release + "-" + Version
)

func printUsage(prog string) {
	fmt.Printf("用法: %s [选项]\n", prog)
	fmt.Println()
	fmt.Println("选项:")
	fmt.Println("  -v, --version   显示版本号")
	fmt.Println("  -h, --help      显示本帮助")
	fmt.Println()
	fmt.Printf("运行: %s          启动交互式 Agent\n", prog)
}

func main() {
	ui.Init()

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "-v":
			ui.Puts(ui.Header, Name+" "+Release+" v"+Version)
			fmt.Println()
			return
		case "--help", "-h":
			printUsage(os.Args[0])
			return
		default:
			fmt.Fprintf(os.Stderr, "未知选项: %s（试试 --help）\n", os.Args[1])
			os.Exit(2)
		}
	}

	// 路径自定位：优先读取可执行文件上一级目录的 config/ 与 skills/
	exeDir := ""
	if exe, err := os.Executable(); err == nil {
		if abs, err2 := filepath.Abs(exe); err2 == nil {
			exeDir = filepath.Dir(abs)
		}
	}
	cfgPath := "config/config.json"
	skillsPath := "skills"
	if exeDir != "" {
		cfgPath = filepath.Join(exeDir, "..", "config", "config.json")
		skillsPath = filepath.Join(exeDir, "..", "skills")
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "请检查 %s 配置文件是否正确\n", cfgPath)
		os.Exit(1)
	}

	limits, err := config.LoadLimits(filepath.Join(filepath.Dir(cfgPath), "limits.json"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "请检查 %s 配置文件是否正确\n", filepath.Join(filepath.Dir(cfgPath), "limits.json"))
		os.Exit(1)
	}

	mgr := skills.New(skillsPath)
	os.Exit(agent.Run(cfg, mgr, limits))
}
