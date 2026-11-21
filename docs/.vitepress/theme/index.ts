// servify docs theme — 默认主题 + 品牌配色（custom.css 见同目录）
// VitePress 会自动发现本文件，无需在 config.mts 里引用。
import DefaultTheme from 'vitepress/theme'
import { defineComponent, h, onMounted } from 'vue'
import './custom.css'

// 内页语言自动探测：与 docs/index.md / docs/zh/index.md 的首页 <script setup>
// 同一契约（localStorage 手动选择优先 > 浏览器语言），仅作用于非首页内页。
// 首页由各页内联脚本处理，这里跳过以免双重跳转；SPA 内导航不重载，故仅直接
// 加载/刷新时探测；dev/preview 的裸路径（非 /servify/ 前缀）一律不动作。
const KEY = 'servify-lang-auto-picked'
const ROOT = '/servify/'
const ZH = '/servify/zh/'

function isZhBrowser(): boolean {
  const langs = navigator.languages?.length ? navigator.languages : [navigator.language]
  return langs.map((l) => l.toLowerCase()).some((l) => l.startsWith('zh'))
}

function detectInnerPageLang(): void {
  if (typeof window === 'undefined') return
  const p = window.location.pathname
  if (p === ROOT || p === ROOT.slice(0, -1) || p === ZH || p === ZH.slice(0, -1)) return
  if (localStorage.getItem(KEY)) return // 有手动选择：内页一律不动
  if (p.startsWith(ZH) && !isZhBrowser()) {
    window.location.replace(ROOT + p.slice(ZH.length))
  } else if (p.startsWith(ROOT) && isZhBrowser()) {
    window.location.replace(ZH + p.slice(ROOT.length))
  }
}

// 记录手动语言选择（与首页脚本同一键、同一判定），使后续内页探测尊重手动选择
if (typeof window !== 'undefined') {
  window.addEventListener(
    'click',
    (e) => {
      const a = (e.target as HTMLElement | null)?.closest?.('a') as HTMLAnchorElement | null
      const href = a?.getAttribute('href') || ''
      if (href.startsWith(ZH) || href.startsWith('zh/')) localStorage.setItem(KEY, ZH)
      else if (href === ROOT || href === ROOT.slice(0, -1)) localStorage.setItem(KEY, ROOT)
    },
    true,
  )
}

export default {
  ...DefaultTheme,
  Layout: defineComponent({
    name: 'ServifyLayout',
    setup(_props, { slots }) {
      onMounted(detectInnerPageLang)
      return () => h(DefaultTheme.Layout, null, { ...slots })
    },
  }),
}
