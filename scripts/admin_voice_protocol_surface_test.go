package scripts

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// 本测试守护管理端坐席语音入口（apps/admin/src/lib/voice.ts + 会话页接线）
// 与 PROTOCOL.md §9 语音翻译通道契约的一致性。
//
// 背景（2026-09-30）：管理端不依赖 sdk 工作区，语音采集/通道/帧消费按 core
// 同口径自带了一份（voice.ts）。自包含实现最大的风险是两处漂移——帧型或
// 端点/采样率/说话方词表偏离 PROTOCOL §9、页面漏接关键下行事件。管理端无
// 测试框架（门禁为 tsc --noEmit + max build，见 ci.yml Admin Checks），故在
// 仓内 scripts 层做静态断言钉住契约；两个测试名已登记进 ci.yml 的
// script-checks -run 白名单（漏登记会被 ci_script_checks_coverage_test.go
// 元守卫拦下）。
//
// 纯静态解析（正则读源码与 PROTOCOL.md），不执行前端代码、不依赖网络，CI 可跑。

const (
	adminVoiceLibPath  = "../apps/admin/src/lib/voice.ts"
	adminVoicePagePath = "../apps/admin/src/pages/Conversation/index.tsx"
	protocolPath       = "../sdk/PROTOCOL.md"
)

// protocolSection 抓 PROTOCOL.md 里指定标题（如 "## 9."）到下一个 "## " 或
// 文件尾之间的正文。
func protocolSection(t *testing.T, heading string) string {
	t.Helper()
	raw, err := os.ReadFile(protocolPath)
	if err != nil {
		t.Fatalf("read %s: %v", protocolPath, err)
	}
	lines := strings.Split(string(raw), "\n")
	start, end := -1, len(lines)
	for i, line := range lines {
		if start == -1 {
			if strings.HasPrefix(line, heading) {
				start = i
			}
			continue
		}
		if strings.HasPrefix(line, "## ") {
			end = i
			break
		}
	}
	if start == -1 {
		t.Fatalf("%s 未找到节标题 %q（PROTOCOL 章节被改名？）", protocolPath, heading)
	}
	return strings.Join(lines[start:end], "\n")
}

// voiceProtocolFrameNames 解析 §9 下行帧载荷表的 type 列（行首 `| \`frame\“）。
func voiceProtocolFrameNames(t *testing.T) []string {
	t.Helper()
	section := protocolSection(t, "## 9.")
	frameRow := regexp.MustCompile(`(?m)^\|\s*` + "`" + `([a-z-]+)` + "`")
	var names []string
	for _, m := range frameRow.FindAllStringSubmatch(section, -1) {
		names = append(names, m[1])
	}
	if len(names) == 0 {
		t.Fatalf("PROTOCOL §9 未解析出任何下行帧型（载荷表格式变了？）")
	}
	sort.Strings(names)
	return names
}

// adminVoiceFrameNames 抓 voice.ts 里出现的全部 `translation-*`/`voice-*`
// 帧型字面量（单双引号皆收；事件名 voice:* 带冒号不落进本正则）。
func adminVoiceFrameNames(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(adminVoiceLibPath)
	if err != nil {
		t.Fatalf("read %s: %v", adminVoiceLibPath, err)
	}
	literal := regexp.MustCompile(`['"]((?:translation|voice)-[a-z]+)['"]`)
	seen := map[string]bool{}
	var names []string
	for _, m := range literal.FindAllStringSubmatch(string(raw), -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			names = append(names, m[1])
		}
	}
	if len(names) == 0 {
		t.Fatalf("%s 未解析出任何帧型字面量（文件被重写？）", adminVoiceLibPath)
	}
	sort.Strings(names)
	return names
}

// TestAdminVoiceEntryFramesMatchProtocolNine：voice.ts 消费的帧型集合必须与
// PROTOCOL §9 下行帧载荷表完全相等——管理端不新增协议面（多出即越权加帧），
// 也不缺帧（少了即下行消费面不完整，字幕/音频/错误任一漏接都是静默降级）。
func TestAdminVoiceEntryFramesMatchProtocolNine(t *testing.T) {
	protocol := voiceProtocolFrameNames(t)
	admin := adminVoiceFrameNames(t)

	protocolSet := map[string]bool{}
	for _, name := range protocol {
		protocolSet[name] = true
	}
	adminSet := map[string]bool{}
	for _, name := range admin {
		adminSet[name] = true
	}

	var extra, missing []string
	for _, name := range admin {
		if !protocolSet[name] {
			extra = append(extra, name)
		}
	}
	for _, name := range protocol {
		if !adminSet[name] {
			missing = append(missing, name)
		}
	}
	for _, name := range extra {
		t.Errorf("管理端 voice.ts 出现 PROTOCOL §9 之外的帧型 %q（不新增协议面）", name)
	}
	for _, name := range missing {
		t.Errorf("管理端 voice.ts 未消费 PROTOCOL §9 下行帧 %q（消费面不完整）", name)
	}
}

// TestAdminVoiceEntryWiringContract：钉住 voice.ts 与会话页的关键接线常量——
// 端点、上行采样率、说话方词表、浏览器采集图在位；页面 import 了语音模块并
// 接齐 delta/final/audio 三类下行事件（error 帧经 voice:error 事件同源）。
// 这些是"接线还在"的最低断言，替换为行为级测试时本守卫随之退役。
func TestAdminVoiceEntryWiringContract(t *testing.T) {
	lib, err := os.ReadFile(adminVoiceLibPath)
	if err != nil {
		t.Fatalf("read %s: %v", adminVoiceLibPath, err)
	}
	page, err := os.ReadFile(adminVoicePagePath)
	if err != nil {
		t.Fatalf("read %s: %v", adminVoicePagePath, err)
	}
	libSrc, pageSrc := string(lib), string(page)

	libContract := []struct {
		frag string
		why  string
	}{
		{"/api/v1/ws/voice", "语音 WS 端点（PROTOCOL §9）"},
		{"24000", "上行采样率 24kHz（PROTOCOL §9）"},
		{"'visitor' | 'agent'", "说话方词表（PROTOCOL §9）"},
		{"createScriptProcessor", "浏览器麦克风采集图（ScriptProcessor 选型）"},
		{"access_token", "握手令牌口（guest_token.required 部署）"},
	}
	for _, c := range libContract {
		if !strings.Contains(libSrc, c.frag) {
			t.Errorf("voice.ts 缺少 %s（%q）", c.why, c.frag)
		}
	}
	pageContract := []struct {
		frag string
		why  string
	}{
		{"from '@/lib/voice'", "会话页 import 语音模块"},
		{"new VoiceChannel(", "构造语音通道"},
		{"new MicCapture(", "启动麦克风采集团"},
		{"channel.on('voice:delta'", "接 translation-delta（在途字幕）"},
		{"channel.on('voice:final'", "接 translation-final（已定句字幕）"},
		{"channel.on('voice:audio'", "接 translation-audio（TTS 播放）"},
		{"channel.on('voice:error'", "接 voice-error（降级提示收线）"},
	}
	for _, c := range pageContract {
		if !strings.Contains(pageSrc, c.frag) {
			t.Errorf("会话页缺少 %s（%q）", c.why, c.frag)
		}
	}
}
