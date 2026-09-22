package config

// config_template_lint_test.go 是 P2-2 的配置模板治理门禁：
// 仓库内所有 config 模板（dev / staging / production / weknora）必须与
// Config 结构的 yaml schema 对齐——
//
//  1. 未知键（拼写错误、已删除字段的残留、手写漂移）直接失败。
//     viper.Unmarshal 对未知键静默忽略，没有本门禁时模板 typo 会
//     无声地回退默认值并把错误语义带进部署。
//  2. production / staging 模板的关键敏感键必须显式存在且走 ${ENV}
//     占位符（database 节、jwt.secret），不允许依赖代码默认值。
//  3. 模板的 environment 声明与文件用途一致。
//
// 新增配置项时：改 Config 结构 → 同步更新四个模板 → 本测试自动覆盖。
// 模板有意省略的非敏感键（走 GetDefaultConfig 默认值）是允许的。

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// repoRoot 指向仓库根（apps/server/internal/config 起向上四级）。
const cfgRepoRoot = "../../../../"

var envPlaceholderRe = regexp.MustCompile(`^\$\{[A-Z][A-Z0-9_]*\}$`)

// schemaNode 是 Config 结构反射出的 yaml schema 树。
type schemaNode struct {
	fields        map[string]*schemaNode // struct 字段：key -> 子节点
	acceptsAnyKey bool                   // map 节点：任意键合法
	wildcard      *schemaNode            // map 值类型的子 schema
}

var knownSensitiveLeafKeys = map[string]bool{
	"password":           true,
	"secret":             true,
	"api_key":            true,
	"secret_access_key":  true,
	"access_key_id":      true,
	"auth_token":         true,
	"static_auth_secret": true, // webrtc.turn 时间限凭据的共享 secret（docs/TURN_DEPLOYMENT.md）
}

// buildSchema 从 Config 类型反射生成 schema 树。
func buildSchema(t *testing.T) *schemaNode {
	t.Helper()
	return buildSchemaNode(reflect.TypeOf(Config{}))
}

func buildSchemaNode(typ reflect.Type) *schemaNode {
	node := &schemaNode{fields: map[string]*schemaNode{}}

	switch typ.Kind() {
	case reflect.Pointer, reflect.Interface:
		return buildSchemaNode(typ.Elem())
	case reflect.Struct:
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if !field.IsExported() {
				continue
			}
			tag := field.Tag.Get("yaml")
			name := strings.Split(tag, ",")[0]
			if name == "" || name == "-" {
				continue
			}
			node.fields[name] = buildSchemaNode(field.Type)
		}
		return node
	case reflect.Map:
		node.acceptsAnyKey = true
		node.wildcard = buildSchemaNode(typ.Elem())
		return node
	case reflect.Slice, reflect.Array:
		elem := typ.Elem()
		if elem.Kind() == reflect.Struct ||
			(elem.Kind() == reflect.Pointer && elem.Elem().Kind() == reflect.Struct) {
			return buildSchemaNode(elem)
		}
		return &schemaNode{} // 标量列表：元素走 default 分支即合法
	default:
		return &schemaNode{} // 标量叶子：schema 存在即合法
	}
}

// checkTemplateKeys 同步遍历模板 yaml 树与 schema 树，返回未知键路径。
func checkTemplateKeys(prefix string, templateNode any, schema *schemaNode, unknown *[]string) {
	switch value := templateNode.(type) {
	case map[string]any:
		if schema.acceptsAnyKey {
			// map 节点：任意键合法，值按 map 值类型继续校验。
			for key, child := range value {
				checkTemplateKeys(prefix+key+".", child, schema.wildcard, unknown)
			}
			return
		}
		for key, child := range value {
			childSchema, ok := schema.fields[key]
			if !ok {
				*unknown = append(*unknown, prefix+key)
				continue
			}
			checkTemplateKeys(prefix+key+".", child, childSchema, unknown)
		}
	case []any:
		for _, item := range value {
			checkTemplateKeys(prefix, item, schema, unknown)
		}
	default:
		// 标量叶子：schema 存在即合法。
	}
}

func loadTemplateTree(t *testing.T, relPath string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(cfgRepoRoot, relPath))
	if err != nil {
		t.Fatalf("read %s: %v", relPath, err)
	}
	var tree map[string]any
	if err := yaml.Unmarshal(raw, &tree); err != nil {
		t.Fatalf("parse %s: %v", relPath, err)
	}
	return tree
}

// TestConfigTemplatesHaveNoUnknownKeys 断言所有模板的键都在 Config 的
// yaml schema 内，杜绝 typo / 残留键被 viper 静默忽略。
func TestConfigTemplatesHaveNoUnknownKeys(t *testing.T) {
	schema := buildSchema(t)

	for _, template := range []string{
		"config.yml",
		"config.staging.example.yml",
		"config.production.secure.example.yml",
		"config.weknora.yml",
	} {
		t.Run(template, func(t *testing.T) {
			var unknown []string
			checkTemplateKeys("", loadTemplateTree(t, template), schema, &unknown)
			if len(unknown) > 0 {
				t.Fatalf("模板 %s 存在 Config schema 之外的未知键（viper 会静默忽略）：\n  %s",
					template, strings.Join(unknown, "\n  "))
			}
		})
	}
}

// TestStageProdTemplatesSecretsUseEnvPlaceholders 断言 staging/production
// 模板的 database 节与关键敏感键显式存在且走 ${ENV} 占位符，
// 不允许把代码默认值（dev secret / dev 密码）带进预生产与生产模板。
func TestStageProdTemplatesSecretsUseEnvPlaceholders(t *testing.T) {
	cases := []struct {
		template string
		env      string
	}{
		{"config.staging.example.yml", "staging"},
		{"config.production.secure.example.yml", "production"},
	}
	for _, tc := range cases {
		t.Run(tc.template, func(t *testing.T) {
			tree := loadTemplateTree(t, tc.template)

			server, ok := tree["server"].(map[string]any)
			if !ok {
				t.Fatalf("%s 缺少 server 节", tc.template)
			}
			if got := server["environment"]; got != tc.env {
				t.Fatalf("%s 的 server.environment = %v，期望 %q（与文件用途一致）", tc.template, got, tc.env)
			}

			// database 节必须显式存在：缺失会回退 GetDefaultConfig 的
			// dev 密码，被启动校验拒绝，模板无法用于真实部署。
			db, ok := tree["database"].(map[string]any)
			if !ok {
				t.Fatalf("%s 缺少 database 节：模板会回退 dev 默认密码并被启动校验拒绝", tc.template)
			}
			assertEnvPlaceholder(t, tc.template, "database.password", db["password"])

			jwt, ok := tree["jwt"].(map[string]any)
			if !ok {
				t.Fatalf("%s 缺少 jwt 节", tc.template)
			}
			assertEnvPlaceholder(t, tc.template, "jwt.secret", jwt["secret"])

			assertNoHardcodedSecrets(t, tc.template, "", tree)
		})
	}
}

func assertEnvPlaceholder(t *testing.T, template, path string, value any) {
	t.Helper()
	s, ok := value.(string)
	if !ok || !envPlaceholderRe.MatchString(s) {
		t.Fatalf("%s 的 %s 必须是 ${ENV_VAR} 占位符（实际值 %v）；示例模板不允许内置真实或默认凭证", template, path, value)
	}
}

// assertNoHardcodedSecrets 递归断言模板中所有疑似敏感叶子键的值都是
// ${ENV} 占位符或空（显式禁用），不允许明文凭证进模板。
func assertNoHardcodedSecrets(t *testing.T, template, prefix string, node any) {
	t.Helper()
	switch value := node.(type) {
	case map[string]any:
		for key, child := range value {
			path := prefix + key
			if knownSensitiveLeafKeys[key] {
				if s, isStr := child.(string); isStr && s != "" && !envPlaceholderRe.MatchString(s) {
					t.Fatalf("%s 的 %s = %q 疑似明文凭证；请改为 ${ENV_VAR} 占位符", template, path, s)
				}
				continue
			}
			assertNoHardcodedSecrets(t, template, path+".", child)
		}
	case []any:
		for _, item := range value {
			assertNoHardcodedSecrets(t, template, prefix, item)
		}
	}
}
