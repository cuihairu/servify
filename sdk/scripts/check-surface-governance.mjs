import fs from "node:fs";
import path from "node:path";

const workspaceRoot = path.resolve(import.meta.dirname, "..");

function mustRead(relativePath) {
  return fs.readFileSync(path.join(workspaceRoot, relativePath), "utf8");
}

function assertIncludes(content, expected, label) {
  if (!content.includes(expected)) {
    throw new Error(`Expected ${label} to include ${JSON.stringify(expected)}`);
  }
}

const governance = mustRead("SURFACE_GOVERNANCE.md");
const sdkReadme = mustRead("README.md");
const reactReadme = mustRead("packages/react/README.md");
const vueReadme = mustRead("packages/vue/README.md");
const vanillaReadme = mustRead("packages/vanilla/README.md");
const reactExampleViteConfig = mustRead("examples/react/vite.config.ts");
const vanillaExample = mustRead("examples/vanilla/index.html");

assertIncludes(governance, "@servify/core", "sdk/SURFACE_GOVERNANCE.md");
assertIncludes(governance, "Breaking Change Checklist", "sdk/SURFACE_GOVERNANCE.md");
assertIncludes(sdkReadme, "Reserved packages now include stable design-time contracts", "sdk/README.md");

assertIncludes(reactReadme, "@servify/react", "sdk/packages/react/README.md");
// 示例经 vite alias 消费 monorepo 发布物（@servify/* 未发布到 npm registry）。
assertIncludes(reactExampleViteConfig, "@servify/react", "sdk/examples/react/vite.config.ts");

assertIncludes(vueReadme, "@servify/vue", "sdk/packages/vue/README.md");
assertIncludes(vanillaReadme, "@servify/vanilla", "sdk/packages/vanilla/README.md");
assertIncludes(vanillaExample, "../../packages/vanilla/dist/index.js", "sdk/examples/vanilla/index.html");

// P0-7（SDK 工程门禁失效修复）回归守卫：typecheck 必须显式绑定仓库内
// TypeScript 编译器，禁止回到不稳定的 `npx tsc` 解析（CI 与本地环境的
// npx 解析结果不一致会把真类型错误淹没在解析噪音里，这正是 P0-7 的
// 根因）；根级 typecheck 链必须与 packages/* 工作区一一对应——新增包
// 漏登记或删除包留死链都会在这里红，门禁聚合不得静默缩小覆盖面。
const EXPECTED_TYPECHECK = "node ../../node_modules/typescript/bin/tsc --noEmit";
const packagesDir = path.join(workspaceRoot, "packages");
const packageDirs = fs
  .readdirSync(packagesDir)
  .filter((entry) => fs.statSync(path.join(packagesDir, entry)).isDirectory())
  .sort();
if (packageDirs.length === 0) {
  throw new Error("Expected at least one workspace package under sdk/packages");
}
const rootPkg = JSON.parse(mustRead("package.json"));
const rootScripts = rootPkg.scripts ?? {};
const rootTypecheck = rootScripts.typecheck ?? "";
for (const dir of packageDirs) {
  const scripts = JSON.parse(mustRead(`packages/${dir}/package.json`)).scripts ?? {};
  if (scripts.typecheck !== EXPECTED_TYPECHECK) {
    throw new Error(
      `packages/${dir}/package.json typecheck must be exactly ${JSON.stringify(EXPECTED_TYPECHECK)} ` +
        `(P0-7: explicit local compiler, no npx resolution); got ${JSON.stringify(scripts.typecheck ?? null)}`,
    );
  }
  if (!rootTypecheck.includes(`npm run typecheck:${dir}`)) {
    throw new Error(`sdk/package.json root typecheck chain is missing workspace "${dir}"`);
  }
  const delegator = rootScripts[`typecheck:${dir}`];
  if (!delegator || !(delegator.includes("--workspace") && delegator.includes("run typecheck"))) {
    throw new Error(
      `sdk/package.json typecheck:${dir} must delegate via "npm --workspace ... run typecheck"; got ${JSON.stringify(delegator ?? null)}`,
    );
  }
}
for (const key of Object.keys(rootScripts).filter((k) => k.startsWith("typecheck:"))) {
  const dir = key.slice("typecheck:".length);
  if (!packageDirs.includes(dir)) {
    throw new Error(`sdk/package.json ${key} has no matching packages/${dir} workspace (stale chain entry)`);
  }
}
if (rootTypecheck.includes("npx ")) {
  throw new Error("sdk/package.json root typecheck must not rely on npx resolution (P0-7)");
}

console.log("SDK surface governance checks passed.");
