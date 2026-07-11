import react from '@vitejs/plugin-react';
import wails from '@wailsio/runtime/plugins/vite';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [react(), wails('./bindings')],
  server: {
    host: '127.0.0.1',
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
