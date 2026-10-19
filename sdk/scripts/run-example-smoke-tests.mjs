import fs from "node:fs";
import path from "node:path";

const workspaceRoot = path.resolve(import.meta.dirname, "..");

function mustRead(filePath) {
  return fs.readFileSync(path.resolve(workspaceRoot, filePath), "utf8");
}

function assertIncludes(content, expected, label) {
  if (!content.includes(expected)) {
    throw new Error(`Expected ${label} to include ${JSON.stringify(expected)}`);
  }
}

function assertExcludes(content, forbidden, label) {
  if (content.includes(forbidden)) {
    throw new Error(`Expected ${label} NOT to include ${JSON.stringify(forbidden)}`);
  }
}

const reactPackage = mustRead("examples/react/package.json");
const reactViteConfig = mustRead("examples/react/vite.config.ts");
const reactMain = mustRead("examples/react/src/main.tsx");
const vuePackage = mustRead("examples/vue/package.json");
const vueViteConfig = mustRead("examples/vue/vite.config.ts");
const vueMain = mustRead("examples/vue/src/main.ts");
const vueApp = mustRead("examples/vue/src/App.vue");
const vanillaHtml = mustRead("examples/vanilla/index.html");

// @servify/* 未发布到 npm registry：示例必须经 vite alias 消费
// monorepo 内的发布物 dist，依赖声明里出现 @servify/* 会让 npm install 404。
assertExcludes(reactPackage, "@servify/react", "sdk/examples/react/package.json dependencies");
assertIncludes(
  reactViteConfig,
  "'@servify/react': fileURLToPath(",
  "sdk/examples/react/vite.config.ts alias",
);
assertIncludes(
  reactViteConfig,
  "../../packages/react/dist/index.esm.js",
  "sdk/examples/react/vite.config.ts alias target",
);

assertExcludes(vuePackage, "@servify/vue", "sdk/examples/vue/package.json dependencies");
assertIncludes(
  vueViteConfig,
  "'@servify/vue': fileURLToPath(",
  "sdk/examples/vue/vite.config.ts alias",
);
assertIncludes(
  vueViteConfig,
  "../../packages/vue/dist/index.esm.js",
  "sdk/examples/vue/vite.config.ts alias target",
);

assertIncludes(reactMain, "ReactDOM.createRoot", "sdk/examples/react/src/main.tsx");
assertIncludes(reactMain, "App", "sdk/examples/react/src/main.tsx");

assertIncludes(vueMain, "ServifyPlugin", "sdk/examples/vue/src/main.ts");
assertIncludes(vueApp, "useRemoteAssist", "sdk/examples/vue/src/App.vue");

assertIncludes(vanillaHtml, "../../packages/vanilla/dist/index.js", "sdk/examples/vanilla/index.html");
assertIncludes(vanillaHtml, "new Servify(", "sdk/examples/vanilla/index.html");
assertIncludes(vanillaHtml, "startChat()", "sdk/examples/vanilla/index.html");

// 纯 REST/WS 最小接入样例：不依赖 @servify/*，必须把第三方接入所需的
// 六件事写全（会话获取、token、WS 握手、历史补拉对账、错误处理、断线重连）。
const restWsHtml = mustRead("examples/rest-ws/index.html");
assertExcludes(restWsHtml, "@servify", "sdk/examples/rest-ws/index.html must not import the SDK");
assertIncludes(restWsHtml, "/api/v1/guest/session", "rest-ws session bootstrap call");
assertIncludes(restWsHtml, "access_token", "rest-ws token handling");
assertIncludes(restWsHtml, "/api/v1/ws", "rest-ws websocket endpoint");
assertIncludes(restWsHtml, "searchParams.set('session_id'", "rest-ws session id handshake param");
assertIncludes(restWsHtml, "?after_id=", "rest-ws history reconcile cursor");
assertIncludes(restWsHtml, "WebSocket", "rest-ws websocket client");
assertIncludes(restWsHtml, "reconnect", "rest-ws reconnect surface");
assertIncludes(restWsHtml, "scheduleReconnect()", "rest-ws exponential backoff");
assertIncludes(restWsHtml, "handleFrameError(", "rest-ws frame level error surface");

console.log("SDK example smoke tests passed.");
