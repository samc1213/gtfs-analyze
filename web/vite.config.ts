import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// In development, /api is proxied to `gtfs-analyze serve` (default localhost:8080).
// Override with GTFS_API_URL, e.g. GTFS_API_URL=http://localhost:8787 npm run dev.
// For a deployed build, set VITE_API_BASE to the API's origin instead.
export default defineConfig({
  plugins: [react()],
  // MapLibre's worker is an ES module
  worker: { format: "es" },
  server: {
    proxy: {
      "/api": process.env.GTFS_API_URL ?? "http://localhost:8080",
    },
  },
});
