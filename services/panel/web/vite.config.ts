import path from "node:path"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vitest/config"

const backend = process.env.PANEL_BACKEND ?? "http://127.0.0.1:58080"

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: { alias: { "@": path.resolve(__dirname, "src") } },
  build: { outDir: "../ui/dist", emptyOutDir: true, sourcemap: false },
  server: {
    host: "127.0.0.1",
    port: 5173,
    strictPort: true,
    proxy: {
      "/api": { target: backend },
      "/auth": { target: backend },
    },
  },
  test: { environment: "jsdom", include: ["src/**/*.test.{ts,tsx}"] },
})
