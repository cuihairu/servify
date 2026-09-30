package scripts

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// P0-8（网站与部署脚本路径失配修复）回归守卫：受控生成物链在仓库里只有一条
// 正规路径——docs/generated/api（generated-assets.manifest 收口 + CI/docs-pages
// drift 校验），swag 版本钉死 v1.16.6。但 Makefile 的 docs 目标曾自成一派：
// `swag init -o docs/`，输出落在 .gitignore 显式登记的散落 byproduct 死路径
// （/docs/docs.go、/docs/swagger.json、/docs/swagger.yaml——gitignore 注释原文
// 「正主在 docs/generated/api/，勿提交」），且用未钉版本的系统 swag——本地跑
// `make docs` 产出的资产与 CI 正规链不同源不同目录，正是 P0-8 登记的失配。
// 本测试把「输出目录 + swag 版本」在四处引用点（Makefile / regenerate 脚本 /
// ci.yml / docs-pages.yml）钉成一处口径，回退即红；顺带钉住 website 三目标的
// 引用路径必须指向真实存在的静态站点。
//
// 只做静态文本解析，不执行 swag/go/网络，CI 可跑。

const (
	makefilePath    = "../Makefile"
	swagPinPrefix   = "github.com/swaggo/swag/cmd/swag@"
	swagOutputDir   = "docs/generated/api"
	generatedAssets = "../generated-assets.manifest"
)

// makefileTargetBlock 提取 Makefile 指定目标的块：目标行（列 0 的 `name:`）起，
// 收进 tab 开头的配方行，止于下一个非空、非注释、非 tab 的行（即下一目标）。
func makefileTargetBlock(t *testing.T, path, target string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	lines := strings.Split(string(raw), "\n")
	start := -1
	for i, line := range lines {
		if line == target+":" {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("%s: 未找到目标 %q", path, target)
	}
	for end := start + 1; end < len(lines); end++ {
		line := lines[end]
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, "\t") {
			return strings.Join(lines[start:end], "\n")
		}
	}
	return strings.Join(lines[start:], "\n")
}

// swagPinnedLines 返回文本里所有调用钉版 swag 的行，及各自解析出的 -o 输出目录。
func swagPinnedLines(t *testing.T, label, text string) map[string]string {
	t.Helper()
	out := make(map[string]string)
	initRe := regexp.MustCompile(regexp.QuoteMeta(swagPinPrefix) + `\S+\s+init\s`)
	for _, line := range strings.Split(text, "\n") {
		if !strings.Contains(line, swagPinPrefix) {
			continue
		}
		// 只收真正的生成调用行（swag@vX.Y.Z init ...），注释里的提及不算。
		if !initRe.MatchString(line) {
			continue
		}
		m := regexp.MustCompile(`\s-o\s+(\S+)`).FindStringSubmatch(line)
		if m == nil {
			t.Fatalf("%s: swag 调用行缺 -o 输出目录：%s", label, strings.TrimSpace(line))
		}
		out[line] = m[1]
	}
	if len(out) == 0 {
		t.Fatalf("%s: 未找到钉版 swag 调用（须含 %s... init）", label, swagPinPrefix)
	}
	return out
}

func TestMakefileDocsTargetMatchesGeneratedAssetsPipeline(t *testing.T) {
	docs := makefileTargetBlock(t, makefilePath, "docs")

	// 1. 输出目录必须逐字符等于 canonical 目录，禁止回到散落死路径。
	makefileDirs := swagPinnedLines(t, "Makefile docs 目标", docs)
	for line, dir := range makefileDirs {
		if dir != swagOutputDir {
			t.Errorf("Makefile docs 目标输出目录 %q != canonical %q（/docs/ 下是 .gitignore 登记的散落 byproduct，产出即死）：\n%s", dir, swagOutputDir, strings.TrimSpace(line))
		}
	}
	// 裸 swag init（未钉版本、依赖 PATH）是失配根因之一，禁止单独出现。
	for _, line := range strings.Split(docs, "\n") {
		if strings.Contains(line, "swag init") && !strings.Contains(line, swagPinPrefix) {
			t.Errorf("Makefile docs 目标含未钉版 swag 调用（必须 %s<vX.Y.Z>）：\n%s", swagPinPrefix, strings.TrimSpace(line))
		}
	}

	// 2. 四处引用点同源同版：Makefile / regenerate 脚本 / ci.yml / docs-pages.yml
	//    的 swag 版本与 -o 目录必须完全一致——任何一处单独升级或改目录都会让
	//    `make docs` 产物与 CI drift 校验的正规链漂移。
	pins := map[string]string{}
	for label, path := range map[string]string{
		"Makefile":                       makefilePath,
		"regenerate-generated-assets.sh": filepath.Join("..", "scripts", "regenerate-generated-assets.sh"),
		"ci.yml":                         filepath.Join("..", ".github", "workflows", "ci.yml"),
		"docs-pages.yml":                 filepath.Join("..", ".github", "workflows", "docs-pages.yml"),
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for line, dir := range swagPinnedLines(t, label, string(raw)) {
			version := regexp.MustCompile(regexp.QuoteMeta(swagPinPrefix) + `(v\S+)`).FindStringSubmatch(line)
			if version == nil {
				t.Fatalf("%s: swag 调用未钉版本：%s", label, strings.TrimSpace(line))
			}
			key := version[1] + " -> " + dir
			pins[key] = label
		}
	}
	if len(pins) != 1 {
		for key, label := range pins {
			t.Errorf("%s: swag 口径 %q", label, key)
		}
		t.Fatalf("swag 版本/输出目录在引用点间不一致（须统一为 %s<vX.Y.Z> -o %s）", swagPinPrefix, swagOutputDir)
	}

	// 3. canonical 目录必须是 manifest 收口对象：三件 swag 产物逐件登记，
	//    与 regenerate 脚本实际产出对应。
	manifest, err := os.ReadFile(generatedAssets)
	if err != nil {
		t.Fatalf("read %s: %v", generatedAssets, err)
	}
	var listed []string
	for _, line := range strings.Split(string(manifest), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			listed = append(listed, line)
		}
	}
	for _, artifact := range []string{"docs.go", "swagger.json", "swagger.yaml"} {
		want := swagOutputDir + "/" + artifact
		found := false
		for _, entry := range listed {
			if entry == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s 未登记进 generated-assets.manifest（docs 目标产出必须被 manifest/CI drift 校验收口）", want)
		}
	}
}

func TestMakefileWebsiteTargetsPointAtStaticSite(t *testing.T) {
	// website 三目标的引用路径必须指向真实存在的静态站点——apps/website 是
	// 无构建步骤的纯静态目录（Cloudflare Pages 构建输出目录同口径），目标里
	// 的路径写成别的名字（如历史遗留的 website-worker / 其他产物目录）即是
	// P0-8 登记的「部署脚本引用路径失配」。
	for _, target := range []string{"website-dev", "website-deploy", "website-pages-deploy"} {
		block := makefileTargetBlock(t, makefilePath, target)
		// token 级精确匹配：引用必须是 apps/website 本身或其子路径
		// （apps/website/wrangler.jsonc）。历史失配形态 apps/website-worker
		// 与 apps/website 前缀相同，Contains 子串检查拦不住，必须逐 token 判。
		for _, tok := range regexp.MustCompile(`apps/website[^\s]*`).FindAllString(block, -1) {
			if tok != "apps/website" && !strings.HasPrefix(tok, "apps/website/") {
				t.Errorf("Makefile %s 目标引用了失配路径 %q（站点产物唯一目录是 apps/website，website-worker 类前缀同名目录即 P0-8 历史形态）", target, tok)
			}
		}
	}

	// 目标引用的关键文件必须真实在位：站点入口、wrangler 配置、404 页。
	for _, rel := range []string{
		"apps/website/index.html",
		"apps/website/404.html",
		"apps/website/wrangler.jsonc",
	} {
		if _, err := os.Stat(filepath.Join("..", rel)); err != nil {
			t.Errorf("站点文件 %s 不存在：Makefile/部署脚本引用了失配路径", rel)
		}
	}

	// wrangler 资产目录必须相对配置文件自身（"."，即 apps/website）——
	// website-deploy 以 --config apps/website/wrangler.jsonc 部署，
	// directory 写成别的相对路径会把空目录或错误目录发上线。
	wrangler, err := os.ReadFile(filepath.Join("..", "apps", "website", "wrangler.jsonc"))
	if err != nil {
		t.Fatalf("read wrangler.jsonc: %v", err)
	}
	if !regexp.MustCompile(`"directory"\s*:\s*"\."`).Match(wrangler) {
		t.Error("apps/website/wrangler.jsonc assets.directory 必须为 \".\"（相对配置文件的 apps/website 自身）")
	}

	// README 部署说明里的「构建输出目录」必须指向真实存在的目录。
	readme, err := os.ReadFile(filepath.Join("..", "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	outDirs := regexp.MustCompile("构建输出目录：`([^`]+)`").FindAllStringSubmatch(string(readme), -1)
	if len(outDirs) == 0 {
		t.Fatal("README.md 未找到「构建输出目录」部署口径（网站部署文档缺失？）")
	}
	for _, m := range outDirs {
		if _, err := os.Stat(filepath.Join("..", m[1])); err != nil {
			t.Errorf("README.md 构建输出目录 %q 不存在（部署文档引用失配路径）", m[1])
		}
	}
}
