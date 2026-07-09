import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [react()],
  server: {
    host: '127.0.0.1',
    port: 5177,
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
