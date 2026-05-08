import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  base: "./",
  plugins: [react()],
  server: {
    port: 18791,
    strictPort: false,
    proxy: {
      "/api": "http://127.0.0.1:18790",
      "/health": "http://127.0.0.1:18790"
    }
  }
});
