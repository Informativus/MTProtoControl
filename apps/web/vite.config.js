import { defineConfig, loadEnv } from 'vite';

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '');

  return {
    server: {
      host: env.WEB_HOST || '0.0.0.0',
      port: Number(env.WEB_PORT || 5173),
      strictPort: true,
    },
  };
});
