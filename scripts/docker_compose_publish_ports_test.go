package scripts

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// 本测试守护 infra/compose 的「主机侧发布端口可覆盖入口」：
//  1. 每条端口映射的宿主机端口必须是 ${VAR:-默认值} 形态（不接受裸端口），
//     否则本机端口被占用（典型：:6379 已被其它 redis 占）时无法起栈，只能改文件；
//  2. 默认值必须与参数化前的历史硬编码一致——覆盖入口不得顺手改动默认行为；
//  3. overlay（weknora/dify）不得重复声明基础栈已发布端口的主机映射，
//     compose 合并是「追加」而非「替换」，重复声明会让同一宿主机端口绑定两次。
//
// 只做静态文本解析，不依赖 docker / docker-compose 是否存在，CI 可跑。

// composePublishPortDefaults 参数化前的历史宿主端口，逐条钉住。
var composePublishPortDefaults = map[string][]string{
	"docker-compose.yml": {
		"POSTGRES_PUBLISH_PORT=5432:5432",
		"REDIS_PUBLISH_PORT=6379:6379",
		"SERVIFY_PUBLISH_PORT=8080:8080",
	},
	"docker-compose.weknora.yml": {
		"ELASTICSEARCH_PUBLISH_PORT=9200:9200",
		"EMBEDDING_PUBLISH_PORT=8001:8001",
		"WEKNORA_API_PUBLISH_PORT=9000:9000",
		"WEKNORA_WEB_PUBLISH_PORT=9001:9001",
	},
	"docker-compose.dify.yml": {
		// dify mock 内部端口 8001，历史对外发布在 5001。
		"DIFY_PUBLISH_PORT=5001:8001",
	},
	"docker-compose.observability.yml": {
		"JAEGER_UI_PUBLISH_PORT=16686:16686",
		"OTEL_OTLP_GRPC_PUBLISH_PORT=4317:4317",
		"OTEL_OTLP_HTTP_PUBLISH_PORT=4318:4318",
	},
}

// composeBasePublishedServices 基础栈里已发布主机端口的服务（overlay 不得重复声明）。
var composeBasePublishedServices = map[string]bool{
	"servify":  true,
	"postgres": true,
	"redis":    true,
}

// hostPortOverridePattern 形如 ${SERVIFY_PUBLISH_PORT:-8080}。
var hostPortOverridePattern = regexp.MustCompile(`^\$\{([A-Z][A-Z0-9_]*):-([^}]+)\}$`)

func composeFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "infra", "compose", "*.yml"))
	if err != nil {
		t.Fatalf("glob compose files: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no compose files found under infra/compose")
	}
	return files
}

// parseComposePorts 逐行取出 ports 序列里的每条映射（已去引行内与行尾注释）。
func parseComposePorts(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var (
		entries    []string
		portIndent = -1
	)
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if portIndent >= 0 {
			if indent > portIndent && strings.HasPrefix(trimmed, "-") {
				entries = append(entries, stripPortComment(trimmed[1:]))
				continue
			}
			// 缩进回退即离开 ports 序列；落到本行继续正常解析。
			portIndent = -1
		}
		if trimmed == "ports:" {
			portIndent = indent
		}
	}
	return entries
}

func stripPortComment(entry string) string {
	// 先缩进后注释再引号：`- "${VAR:-1}:1"  # 说明` 这类行三步都要走。
	entry = strings.TrimSpace(entry)
	if idx := strings.Index(entry, " #"); idx >= 0 {
		entry = strings.TrimSpace(entry[:idx])
	}
	return strings.Trim(entry, `"'`)
}

func TestComposeHostPortsAreOverridableWithUnchangedDefaults(t *testing.T) {
	seen := map[string][]string{}
	for _, path := range composeFiles(t) {
		name := filepath.Base(path)
		entries := parseComposePorts(t, path)
		expected, ok := composePublishPortDefaults[name]
		if !ok {
			// coturn 走 network_mode: host（无发布端口），允许缺席登记表；
			// 一旦它新增 ports 就必须登记，否则该断言失败。
			if len(entries) > 0 {
				t.Errorf("%s 含 %d 条主机发布端口但未登记进 composePublishPortDefaults", name, len(entries))
			}
			continue
		}
		var actual []string
		for _, entry := range entries {
			// 宿主机侧本身含 `${VAR:-8080}`（内含冒号），故按最后一个冒号切分。
			sep := strings.LastIndex(entry, ":")
			if sep < 0 {
				t.Errorf("%s: 端口映射 %q 无法解析 host:container", name, entry)
				continue
			}
			host, container := entry[:sep], entry[sep+1:]
			match := hostPortOverridePattern.FindStringSubmatch(strings.TrimSpace(host))
			if match == nil {
				t.Errorf("%s: 端口映射 %q 的宿主机端口不可覆盖，应为 ${VAR:-默认端口} 形态", name, entry)
				continue
			}
			varName, defaultPort := match[1], match[2]
			if _, err := strconv.Atoi(defaultPort); err != nil {
				t.Errorf("%s: ${%s:-%s} 默认值不是端口号", name, varName, defaultPort)
			}
			if !strings.HasSuffix(varName, "_PUBLISH_PORT") {
				t.Errorf("%s: 变量 %s 未遵循 *_PUBLISH_PORT 命名", name, varName)
			}
			actual = append(actual, varName+"="+defaultPort+":"+container)
		}
		sort.Strings(actual)
		want := append([]string(nil), expected...)
		sort.Strings(want)
		if strings.Join(actual, "|") != strings.Join(want, "|") {
			t.Errorf("%s: 可覆盖端口入口与预期不一致\n got: %v\nwant: %v", name, actual, want)
		}
		seen[name] = actual
	}
	total := 0
	for _, entries := range seen {
		total += len(entries)
	}
	if total != 11 {
		t.Errorf("可覆盖发布端口入口总数应为 11（基础 3 + weknora 4 + dify 1 + observability 3），实际 %d", total)
	}
}

func TestComposeOverlaysDoNotRedeclareBasePorts(t *testing.T) {
	for _, name := range []string{"docker-compose.weknora.yml", "docker-compose.dify.yml"} {
		path := filepath.Join("..", "infra", "compose", name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var (
			svc        string
			indent     = -1
			badPorts   []string
			inPorts    = false
			portIndent = -1
		)
		for _, line := range strings.Split(string(raw), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			cur := len(line) - len(strings.TrimLeft(line, " "))
			if inPorts {
				if cur > portIndent && strings.HasPrefix(trimmed, "-") {
					if composeBasePublishedServices[svc] {
						badPorts = append(badPorts, svc+": "+stripPortComment(trimmed[1:]))
					}
					continue
				}
				inPorts = false
			}
			if cur == 4 && strings.HasSuffix(trimmed, ":") && !strings.Contains(trimmed, " ") {
				svc = strings.TrimSuffix(trimmed, ":")
				indent = cur
			}
			if trimmed == "ports:" && indent >= 0 && cur > indent {
				inPorts = true
				portIndent = cur
			}
		}
		if len(badPorts) > 0 {
			t.Errorf("%s: overlay 重复声明了基础栈服务的发布端口（compose 端口列表按追加合并，会重复绑定同一宿主端口）：%v", name, badPorts)
		}
	}
}
