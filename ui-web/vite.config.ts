import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// https://vite.dev/config/
export default defineConfig(() => {
  const apiTarget = process.env.VITE_API_PROXY_TARGET || 'http://127.0.0.1:17321';
  const trustLocalProxyOrigin = process.env.VITE_TRUST_LOCAL_PROXY_ORIGIN === '1';
  return {
    plugins: [react(), tailwindcss()],
    build: {
      modulePreload: { polyfill: false },
      // 把稳定的大依赖拆到独立 chunk：页面代码更新时这些仍能命中浏览器缓存。
      rollupOptions: {
        output: {
          manualChunks(id: string) {
            if (!id.includes('node_modules')) return undefined;
            if (/node_modules\/(react|react-dom|scheduler)\//.test(id)) return 'react';
            if (/node_modules\/(react-markdown|remark-|micromark|mdast-|unist-|hast-|unified|vfile|bail|trough|is-plain-obj|decode-named-character-reference|character-entities|trim-lines|space-separated-tokens|comma-separated-tokens|property-information|html-url-attributes|devlop|longest-streak|zwitch|ccount|markdown-table|escape-string-regexp|extend|style-to-object|inline-style-parser)\//.test(id)) return 'markdown';
            if (/node_modules\/swr\//.test(id)) return 'swr';
            return undefined;
          },
        },
      },
    },
    server: {
      proxy: {
        '/api': {
          target: apiTarget,
          changeOrigin: true,
          // E2E runs Vite and the loopback API on separate ports. Production
          // Electron never enables this; its renderer is already same-origin.
          configure: trustLocalProxyOrigin ? (proxy) => {
            proxy.on('proxyReq', (proxyRequest, request) => {
              if (request.headers.origin) proxyRequest.setHeader('origin', apiTarget);
            });
          } : undefined,
        },
      },
    },
  };
})
