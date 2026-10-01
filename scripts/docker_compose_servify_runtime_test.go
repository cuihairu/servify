package scripts

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// 本测试守护 compose 模板 servify 服务定义与运行时镜像（Dockerfile 末阶段）
// 的两处同步点——2026-09-29 全容器化 weknora 验收发现的两处潜伏缺陷，修正
// 后钉住，防止回退：
//  1. healthcheck 探针：运行时镜像 alpine:latest 只装 ca-certificates，没有
//     curl——用 curl 的探针必然 exec 失败、容器被判 unhealthy。探针必须用
//     alpine 自带的 busybox wget（非 2xx 同样非零退出，语义与 curl -f 一致）。
//  2. 挂载路径：服务把日志写 ./logs/servify.log、附件写 ./uploads，配置约定
//     挂载点 ./config.yml，全部相对进程 CWD（= 镜像末阶段 WORKDIR）解析——
//     挂载目标必须落在 WORKDIR 下，挂到别处宿主目录永远收不到数据（容器
//     重建即丢）。
//
// 同时交叉校验 compose 挂载前缀与 Dockerfile 末阶段 WORKDIR 一致：改镜像
// WORKDIR 而不同步改 compose 挂载（或反之）会在这里失败。
//
// 2026-10-01 覆盖面从基础模板扩到 infra/compose 全部 compose 文件（对齐
// docker_compose_publish_ports_test.go 的 glob 全文件惯例）：overlay 的
// healthcheck 按合并语义整体替换基础栈探针、volumes 按容器路径追加/覆盖——
// 只扫基础文件的守卫挡不住 overlay 重新引入 curl 探针或错位挂载，两处缺陷
// 会在 overlay 路径上原样复发。
//
// 只做静态文本解析，不依赖 docker / docker-compose 是否存在，CI 可跑。

// composeServifyBindMounts servify 服务三条宿主挂载逐条钉住（容器内路径均须
// 落在镜像末阶段 WORKDIR 下）。
var composeServifyBindMounts = []string{
	"../../logs:/root/logs",
	"../../uploads:/root/uploads",
	"../../config.yml:/root/config.yml:ro",
}

// composeServiceBlock 返回 compose 文件里指定服务（两空格缩进的键）的文本块，
// 含键行、止于下一个同级键或文件尾；文件不含该服务即失败。
func composeServiceBlock(t *testing.T, path, service string) string {
	t.Helper()
	block, ok := composeServiceBlockOptional(t, path, service)
	if !ok {
		t.Fatalf("%s: 未找到服务定义 %q", path, service)
	}
	return block
}

// composeServiceBlockOptional 同 composeServiceBlock，但文件不含该服务时返回
// ("", false)——overlay 可以不声明 servify（observability 只挂监控面）。
func composeServiceBlockOptional(t *testing.T, path, service string) (string, bool) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	lines := strings.Split(string(raw), "\n")
	start := -1
	for i, line := range lines {
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 2 && strings.TrimSpace(line) == service+":" {
			start = i
			break
		}
	}
	if start < 0 {
		return "", false
	}
	for end := start + 1; end < len(lines); end++ {
		line := lines[end]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if indent := len(line) - len(strings.TrimLeft(line, " ")); indent <= 2 {
			return strings.Join(lines[start:end], "\n"), true
		}
	}
	return strings.Join(lines[start:], "\n"), true
}

// composeBlockEntryLines 取块内某键（如 volumes:/healthcheck:）下属的缩进
// 条目行原文，缩进回退即离块。返回「行内去掉 "- " 前缀、去引行注释与引号」
// 后的条目（与 parseComposePorts 同一套清洗），以及「key: value」原行。
func composeBlockEntryLines(t *testing.T, block, key string) []string {
	t.Helper()
	var (
		entries   []string
		keyIndent = -1
	)
	for _, line := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if keyIndent >= 0 {
			if indent > keyIndent {
				trimmed = strings.TrimPrefix(trimmed, "- ")
				entries = append(entries, stripPortComment(trimmed))
				continue
			}
			keyIndent = -1
		}
		if trimmed == key+":" {
			keyIndent = indent
		}
	}
	return entries
}

// dockerfileFinalStageWorkdir 读出 Dockerfile 末阶段的最后一条 WORKDIR。
func dockerfileFinalStageWorkdir(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	inFinal := false
	workdir := ""
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		switch {
		case strings.EqualFold(fields[0], "FROM"):
			inFinal = true
			workdir = ""
		case inFinal && strings.EqualFold(fields[0], "WORKDIR") && len(fields) > 1:
			workdir = fields[1]
		}
	}
	if workdir == "" {
		t.Fatalf("%s: 末阶段未声明 WORKDIR", path)
	}
	return strings.TrimSuffix(workdir, "/")
}

// containerSide 拆短格式挂载条目的容器内路径（第二段；宿主相对路径不含冒号，
// 冒号只可能出现在容器路径与选项边界）。
func containerSide(entry string) string {
	parts := strings.Split(entry, ":")
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

func TestComposeServifyHealthcheckProbeExistsInRuntimeImage(t *testing.T) {
	block := composeServiceBlock(t, filepath.Join("..", "infra", "compose", "docker-compose.yml"), "servify")
	probes := composeBlockEntryLines(t, block, "healthcheck")
	if len(probes) == 0 {
		t.Fatal("servify 服务缺 healthcheck 定义")
	}
	var probe string
	for _, line := range probes {
		if strings.HasPrefix(line, "test:") {
			probe = strings.TrimSpace(strings.TrimPrefix(line, "test:"))
			break
		}
	}
	if probe == "" {
		t.Fatal("servify healthcheck 缺 test 探针")
	}
	if !strings.Contains(probe, "wget") {
		t.Errorf("healthcheck 探针 %s 必须用 wget（alpine 运行时镜像没装 curl，curl 探针必然 exec 失败）", probe)
	}
	if strings.Contains(probe, "curl") {
		t.Errorf("healthcheck 探针 %s 引用了运行时镜像不存在的 curl", probe)
	}
	if !strings.Contains(probe, "8080/health") {
		t.Errorf("healthcheck 探针 %s 未指向容器内 8080/health", probe)
	}
}

func TestComposeServifyMountsLiveUnderImageWorkdir(t *testing.T) {
	const (
		composePath = "../infra/compose/docker-compose.yml"
		dockerfile  = "../Dockerfile"
	)
	workdir := dockerfileFinalStageWorkdir(t, dockerfile)

	block := composeServiceBlock(t, composePath, "servify")
	entries := composeBlockEntryLines(t, block, "volumes")
	if len(entries) == 0 {
		t.Fatal("servify 服务缺 volumes 定义")
	}
	var actual []string
	for _, entry := range entries {
		if !strings.HasPrefix(entry, ".") && !strings.HasPrefix(entry, "/") {
			t.Errorf("servify 挂载 %q 不是宿主路径 bind mount（长格式或命名卷需同步更新本测试）", entry)
			continue
		}
		container := containerSide(entry)
		if !strings.HasPrefix(container, workdir+"/") && container != workdir {
			t.Errorf("servify 挂载 %q 容器内路径 %q 不在镜像末阶段 WORKDIR %q 下：服务按进程 CWD 相对路径写日志/附件/读配置，挂别处宿主收不到数据", entry, container, workdir)
		}
		actual = append(actual, entry)
	}
	sort.Strings(actual)
	want := append([]string(nil), composeServifyBindMounts...)
	sort.Strings(want)
	if strings.Join(actual, "|") != strings.Join(want, "|") {
		t.Errorf("servify 挂载与预期不一致（改动须同步更新 composeServifyBindMounts 并保持容器内路径落在镜像 WORKDIR 下）\n got: %v\nwant: %v", actual, want)
	}
}

// TestComposeServifyRuntimeContractHoldsInEveryComposeFile 把 healthcheck 探针
// 与宿主挂载落位两条契约钉到 infra/compose 的每一个 compose 文件上（glob 全
// 文件，与 publish_ports 守卫同惯例）：overlay 一旦声明 servify 的
// healthcheck（test 键按合并语义整体替换基础栈探针）或 volumes（按容器路径
// 追加/覆盖），只扫基础文件的两条既有守卫拦不住，curl 探针与 WORKDIR 外挂载
// 会在 overlay 起栈路径上原样复发。声明处即校验处，未声明则由基础模板守卫兜底。
func TestComposeServifyRuntimeContractHoldsInEveryComposeFile(t *testing.T) {
	workdir := dockerfileFinalStageWorkdir(t, "../Dockerfile")
	declaredHealthcheck := 0
	for _, path := range composeFiles(t) {
		name := filepath.Base(path)
		block, ok := composeServiceBlockOptional(t, path, "servify")
		if !ok {
			continue // overlay 可以不声明 servify 服务
		}
		// 1. healthcheck：声明处的探针必须满足运行时镜像口径（alpine 只装
		//    ca-certificates，探针只能用自带 busybox wget，且须指到 8080/health）。
		if probes := composeBlockEntryLines(t, block, "healthcheck"); len(probes) > 0 {
			declaredHealthcheck++
			var probe string
			for _, line := range probes {
				if strings.HasPrefix(line, "test:") {
					probe = strings.TrimSpace(strings.TrimPrefix(line, "test:"))
					break
				}
			}
			if probe == "" {
				t.Errorf("%s: servify healthcheck 声明了但缺 test 探针（overlay 合并只覆盖写的键，缺 test 即沿用基础栈探针——要么不声明）", name)
			} else {
				if strings.Contains(probe, "curl") {
					t.Errorf("%s: servify healthcheck 探针 %s 引用了运行时镜像不存在的 curl（alpine:latest 只装 ca-certificates，探针必然 exec 失败、容器被判 unhealthy）", name, probe)
				}
				if !strings.Contains(probe, "wget") {
					t.Errorf("%s: servify healthcheck 探针 %s 必须用 alpine 自带的 busybox wget（非 2xx 同样非零退出，语义与 curl -f 一致）", name, probe)
				}
				if !strings.Contains(probe, "8080/health") {
					t.Errorf("%s: servify healthcheck 探针 %s 未指向容器内 8080/health", name, probe)
				}
			}
		}
		// 2. 宿主 bind 挂载：声明处每条的容器内路径必须落在镜像末阶段 WORKDIR
		//    下（服务按进程 CWD 相对路径写日志/附件/读配置）；命名卷非宿主路径，
		//    不属本缺陷类，跳过。
		for _, entry := range composeBlockEntryLines(t, block, "volumes") {
			if !strings.HasPrefix(entry, ".") && !strings.HasPrefix(entry, "/") {
				continue
			}
			container := containerSide(entry)
			if container == "" {
				t.Errorf("%s: servify 挂载 %q 无法解析容器内路径（长格式需同步更新本测试）", name, entry)
				continue
			}
			if !strings.HasPrefix(container, workdir+"/") && container != workdir {
				t.Errorf("%s: servify 挂载 %q 容器内路径 %q 不在镜像末阶段 WORKDIR %q 下：服务按进程 CWD 相对路径写日志/附件/读配置，挂别处宿主收不到数据（容器重建即丢）", name, entry, container, workdir)
			}
		}
	}
	// 基础模板另有「必须声明探针」的专项守卫；这里守住扫描面不空转——一个声明处
	// 都没有说明 glob 或服务块解析失灵，本守卫会假绿。overlay 额外声明探针即在
	// 替换基础探针，其契约由上面的逐处校验兜住。
	if declaredHealthcheck == 0 {
		t.Error("没有任何 compose 文件声明 servify healthcheck：探针契约本守卫无一校验（基础模板声明丢失或扫描失灵），专项守卫与本守卫须同步排查")
	}
}
