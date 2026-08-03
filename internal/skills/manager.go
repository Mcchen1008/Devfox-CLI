package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Skill 一个技能（对应 C 版 skill_t）
type Skill struct {
	Name          string
	Description   string
	Path          string
	UserInvocable bool
}

// Manager 技能管理器（扫描 skills/ 目录 SKILL.md）
type Manager struct {
	Dir   string
	Items []Skill
}

func New(dir string) *Manager {
	m := &Manager{Dir: dir}
	m.Refresh()
	return m
}

func (m *Manager) Refresh() {
	m.Items = nil
	if m.Dir == "" {
		m.Dir = "skills"
	}
	filepath.Walk(m.Dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && info.Name() == "SKILL.md" {
			if sk := parseSkill(path); sk != nil {
				m.Items = append(m.Items, *sk)
			}
		}
		return nil
	})
	sort.Slice(m.Items, func(i, j int) bool {
		return m.Items[i].Name < m.Items[j].Name
	})
}

// parseSkill 解析 SKILL.md frontmatter：
// ---
// name: xxx
// description: xxx
// user-invocable: true
// ---
func parseSkill(path string) *Skill {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	text := string(data)
	sk := &Skill{Path: path, UserInvocable: true}
	if strings.HasPrefix(text, "---") {
		rest := text[3:]
		if idx := strings.Index(rest, "---"); idx >= 0 {
			fm := rest[:idx]
			for _, line := range strings.Split(fm, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "name:") {
					sk.Name = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
				} else if strings.HasPrefix(line, "description:") {
					sk.Description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
				} else if strings.HasPrefix(line, "user-invocable:") {
					v := strings.TrimSpace(strings.TrimPrefix(line, "user-invocable:"))
					sk.UserInvocable = v == "true" || v == "True" || v == "1"
				}
			}
		}
	}
	if sk.Name == "" {
		sk.Name = strings.TrimSuffix(filepath.Base(filepath.Dir(path)), ".md")
	}
	return sk
}

func (m *Manager) Get(name string) *Skill {
	for i := range m.Items {
		if m.Items[i].Name == name {
			return &m.Items[i]
		}
	}
	return nil
}

// Summary 系统提示词中的技能摘要
func (m *Manager) Summary() string {
	var sb strings.Builder
	any := false
	for i := range m.Items {
		if !m.Items[i].UserInvocable {
			continue
		}
		if any {
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, "- **%s**：%s", m.Items[i].Name, m.Items[i].Description)
		any = true
	}
	if !any {
		return "（当前没有可用的技能，请直接回答或使用基础工具）"
	}
	return sb.String()
}

// Names use_skill 工具的可选值列表
func (m *Manager) Names() string {
	names := make([]string, 0, len(m.Items))
	for i := range m.Items {
		if m.Items[i].UserInvocable {
			names = append(names, m.Items[i].Name)
		}
	}
	if len(names) == 0 {
		return "（无）"
	}
	return strings.Join(names, ", ")
}

// Use 加载技能文档
func (m *Manager) Use(name string) string {
	sk := m.Get(name)
	if sk == nil {
		return fmt.Sprintf("✗ 技能不存在: %s。可用技能: %s", name, m.Names())
	}
	data, err := os.ReadFile(sk.Path)
	if err != nil {
		return fmt.Sprintf("✗ 技能文档读取失败: %s", sk.Path)
	}
	return fmt.Sprintf("✓ 技能 [%s] 已加载，以下是完整说明文档：\n\n--- SKILL.md 内容开始 ---\n%s\n--- SKILL.md 内容结束 ---\n\n请严格遵循该技能的说明来完成用户的任务。",
		sk.Name, string(data))
}

func (m *Manager) ListText() string {
	if len(m.Items) == 0 {
		return "当前没有已加载的技能（可用 /addskill 添加）"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "已加载 %d 个技能:\n", len(m.Items))
	for i := range m.Items {
		fmt.Fprintf(&sb, "  %s - %s\n", m.Items[i].Name, m.Items[i].Description)
	}
	fmt.Fprintf(&sb, "\n  技能目录: %s", m.Dir)
	return sb.String()
}
