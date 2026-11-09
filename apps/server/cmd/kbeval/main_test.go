package main

// main() 覆盖按仓库 main 包先例以子进程 re-exec 驱动（cmd/gen-baseline 同款）。

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestKbevalMainSubprocess 在子进程内运行 main()：os.Args 整体替换（测试
// 二进制的 -test.* 旗标不能进 flag.Parse），参数由环境变量注入。
func TestKbevalMainSubprocess(t *testing.T) {
	if os.Getenv("KBEVAL_SUBPROCESS") != "1" {
		return
	}
	args := []string{"kbeval"}
	if os.Getenv("KBEVAL_TEST_JSON") == "1" {
		args = append(args, "-json")
	}
	if dataset := os.Getenv("KBEVAL_TEST_DATASET"); dataset != "" {
		args = append(args, "-dataset="+dataset)
	}
	os.Args = args
	main()
	os.Exit(0)
}

func TestMainHappyPath(t *testing.T) {
	out := runSubprocess(t, "", "")
	if !strings.Contains(out, "knowledge evaluation baseline") {
		t.Fatalf("unexpected main output: %s", out)
	}
}

func TestMainJSONOutput(t *testing.T) {
	out := runSubprocess(t, "1", "")
	if !strings.Contains(out, `"refusal_misjudge_rate"`) {
		t.Fatalf("json main output missing report fields: %s", out)
	}
}

func TestMainMissingDatasetFails(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestKbevalMainSubprocess$")
	cmd.Env = append(os.Environ(),
		"KBEVAL_SUBPROCESS=1",
		"KBEVAL_TEST_DATASET=/nonexistent/dataset.json",
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("missing dataset must exit non-zero, output: %s", out)
	}
	if !strings.Contains(string(out), "read dataset /nonexistent/dataset.json") {
		t.Fatalf("unexpected fatal output: %s", out)
	}
}

func runSubprocess(t *testing.T, jsonEnv, dataset string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestKbevalMainSubprocess$")
	cmd.Env = append(os.Environ(),
		"KBEVAL_SUBPROCESS=1",
		"KBEVAL_TEST_JSON="+jsonEnv,
		"KBEVAL_TEST_DATASET="+dataset,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("main subprocess failed: %v, output: %s", err, out)
	}
	return string(out)
}
