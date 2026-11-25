// 管理端 SPA 静态服务 + /api 反代（浏览器验收用）。
//
// 目的：Go 服务不挂载 admin SPA（无 catch-all），而 dist 用相对 /api 路径，
// 故起同源代理：非 /api 路径 → 静态服务 dist（browser-history 回退 index.html），
// /api* → 转发到真实服务端。
//
// 用法: node admin-browser-proxy.mjs <dist-dir> <admin-port> <servify-url>
import { createServer } from 'http';
import { readFile, stat } from 'fs/promises';
import { join, normalize, extname } from 'path';
import { request } from 'http';

const [, , DIST, ADMIN_PORT, SERVIFY_URL] = process.argv;

const MIME = {
  '.html': 'text/html; charset=utf-8', '.js': 'application/javascript',
  '.css': 'text/css', '.json': 'application/json', '.svg': 'image/svg+xml',
  '.png': 'image/png', '.ico': 'image/x-icon', '.woff2': 'font/woff2',
  '.map': 'application/json',
};

const server = createServer((req, res) => {
  const urlPath = decodeURIComponent(new URL(req.url, 'http://x').pathname);

  // /api 反代（含 /api/v1）
  if (urlPath === '/api' || urlPath.startsWith('/api/') || urlPath === '/health' || urlPath === '/ready') {
    const target = new URL(SERVIFY_URL + urlPath + (new URL(req.url, 'http://x').search || ''));
    const opts = {
      method: req.method,
      hostname: target.hostname,
      port: target.port,
      path: target.pathname + target.search,
      headers: { ...req.headers, host: target.host },
    };
    const upstream = request(opts, (ur) => {
      res.writeHead(ur.statusCode, ur.headers);
      ur.pipe(res);
    });
    upstream.on('error', () => { res.writeHead(502); res.end('bad gateway'); });
    req.pipe(upstream);
    return;
  }

  // 静态服务（SPA 回退）
  let rel = normalize(urlPath).replace(/^([/\\])+/, '');
  if (!rel || rel === '.' || extname(rel) === '') rel = 'index.html';
  let file = join(DIST, rel);
  (async () => {
    try {
      const st = await stat(file);
      if (st.isDirectory()) file = join(file, 'index.html');
      const body = await readFile(file);
      res.writeHead(200, { 'Content-Type': MIME[extname(file)] || 'application/octet-stream' });
      res.end(body);
    } catch {
      // browser-history 回退：非 API 路径一律回 index.html（SPA 路由）
      try {
        const body = await readFile(join(DIST, 'index.html'));
        res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
        res.end(body);
      } catch {
        res.writeHead(404); res.end('not found');
      }
    }
  })();
});

server.listen(Number(ADMIN_PORT), '127.0.0.1', () => {
  console.log(`admin-proxy up: dist=${DIST} port=${ADMIN_PORT} upstream=${SERVIFY_URL}`);
});
