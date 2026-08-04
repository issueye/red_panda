import react from '@vitejs/plugin-react';
import wails from '@wailsio/runtime/plugins/vite';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [react(), wails('./bindings')],
  server: {
    // 用 :: 同时监听 IPv4 与 IPv6。Wails 等待 http://localhost:9245，而 Windows 上
    // localhost 会解析为 ::1（IPv6）与 127.0.0.1（IPv4）；若只监听其一，Go 客户端
    // 优先解析到不可达的地址会导致连接失败（见 GH#5059/#4905）。
    host: '::',
    port: Number(process.env.WAILS_VITE_PORT) || 5177,
    strictPort: false,
    proxy: {
      '/__red_panda_gateway': {
        target: process.env.VITE_RED_PANDA_GATEWAY_PROXY_TARGET || 'http://127.0.0.1:17888',
        changeOrigin: true,
        ws: true,
        configure: (proxy) => {
          proxy.on('error', (error) => {
            if (error?.code !== 'ECONNRESET') {
              console.error(error);
            }
          });
        },
        rewrite: (path) => path.replace(/^\/__red_panda_gateway/, ''),
      },
    },
  },
});
