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

console.log("SDK surface governance checks passed.");
