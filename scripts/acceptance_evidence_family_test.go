package scripts

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// evidenceDirExemptions 列出不参与「脚本→证据入库」家族门禁的证据目录，
// 每项附豁免理由（新目录进豁免清单须带同类理由）。
var evidenceDirExemptions = map[string]string{
	// dify 真实模式需外部 Dify 凭证（todo P1-1 外部阻塞），mock 路径已由
	// TestDifyIntegrationScriptMockModeWritesEvidence / ...PersistsFailureEvidence 覆盖。
	"dify-acceptance": "external dify credentials (P1-1); mock path covered by dify script tests",
	// 遗留知识库验收脚本，已被 test-local-knowledge-acceptance.sh 取代，不再跑。
	"knowledge-acceptance": "legacy script superseded by test-local-knowledge-acceptance.sh",
}

// evidenceDirDeclRe 匹配脚本里 EVIDENCE_DIR 的默认值声明（已知三种形态：
// EVIDENCE_DIR=${EVIDENCE_DIR:-"..."}、EVIDENCE_DIR="${EVIDENCE_DIR:-...}"、
// 内层路径 $PROJECT_ROOT/...、${PROJECT_ROOT}/...、./...；出现新形态时在此登记）。
var evidenceDirDeclRe = regexp.MustCompile(`(?m)^EVIDENCE_DIR="?\$\{EVIDENCE_DIR:-"?(\$\{PROJECT_ROOT\}|\$PROJECT_ROOT|\.)/scripts/test-results/([A-Za-z0-9_-]+)`)

// TestAcceptanceScriptEvidenceFamily 把验收脚本家族钉在
// 「manifest 约定 + validator 分支 + 证据入库」三件套上：每个声明
// EVIDENCE_DIR 的 acceptance 脚本必须有已入库（git tracked）的
// manifest.json，且 validate-acceptance-manifest.sh 存在对应 provider
// 分支（CI 的 check-acceptance-evidence.sh 已对所有入库 manifest 跑
// validator，这里补齐脚本→证据覆盖面：新脚本漏 manifest/分支/证据即红）。
func TestAcceptanceScriptEvidenceFamily(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("git-backed acceptance evidence checks are not stable on Windows")
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read scripts dir: %v", err)
	}

	dirs := map[string]string{} // dir -> declaring script
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "test-") || !strings.HasSuffix(name, ".sh") {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, match := range evidenceDirDeclRe.FindAllStringSubmatch(string(raw), -1) {
			if prev, ok := dirs[match[2]]; ok && prev != name {
				t.Fatalf("evidence dir %q declared by multiple scripts: %s and %s", match[2], prev, name)
			}
			dirs[match[2]] = name
		}
	}
	if len(dirs) == 0 {
		t.Fatal("no evidence dir declarations found in test scripts")
	}

	validatorRaw, err := os.ReadFile("validate-acceptance-manifest.sh")
	if err != nil {
		t.Fatalf("read validator: %v", err)
	}

	var problems []string
	for dir, script := range dirs {
		if reason, exempt := evidenceDirExemptions[dir]; exempt {
			t.Logf("exempt %s (%s): %s", dir, script, reason)
			continue
		}
		manifestPath := filepath.Join("test-results", dir, "manifest.json")
		if _, err := os.Stat(manifestPath); err != nil {
			problems = append(problems, dir+": missing "+manifestPath+" (run "+script+" to generate)")
			continue
		}
		tracked, err := exec.Command("git", "ls-files", "--", filepath.Join("test-results", dir, "manifest.json")).Output()
		if err != nil {
			t.Fatalf("git ls-files for %s: %v", dir, err)
		}
		if strings.TrimSpace(string(tracked)) == "" {
			problems = append(problems, dir+": manifest.json not tracked in git (commit the evidence)")
			continue
		}
		raw, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatalf("read %s: %v", manifestPath, err)
		}
		var manifest struct {
			Provider string `json:"provider"`
		}
		if err := json.Unmarshal(raw, &manifest); err != nil {
			problems = append(problems, dir+": manifest.json is not valid JSON: "+err.Error())
			continue
		}
		if manifest.Provider == "" {
			problems = append(problems, dir+": manifest.json has empty provider")
			continue
		}
		branchRe := regexp.MustCompile(`(?m)^\s+` + regexp.QuoteMeta(manifest.Provider) + `\)\s*$`)
		if !branchRe.Match(validatorRaw) {
			problems = append(problems, dir+": validate-acceptance-manifest.sh has no branch for provider "+manifest.Provider)
		}
	}

	if len(problems) > 0 {
		t.Fatalf("acceptance evidence family violations:\n  %s", strings.Join(problems, "\n  "))
	}
}
