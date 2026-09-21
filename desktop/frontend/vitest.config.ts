// The behavior tests run in a jsdom window, the same offline surface Wails
// renders into. Nothing here reaches a network: the suite has no fetch, the
// components make none, and the stubbed facade is an in-memory object.
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    setupFiles: ["./vitest.setup.ts"],
  },
});
