// The interaction journeys: the production App tree driven by real user events
// against the real internal/desktop facade over real files. They run apart
// from the component tests because every journey needs the Go toolchain that
// builds the bridge, and each one is slower than a component test. No
// journey reaches beyond this machine: the bridge serves the facade over
// standard input and output, the guided sample's practice receiver is the
// application's own on loopback, and every file a journey touches is inside
// its own temporary root. Building the bridge and the command line is an ordinary Go build of
// this checkout, which resolves the pinned modules as any build does.
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    include: ["src/journeys/**/*.journey.tsx"],
    setupFiles: ["./vitest.setup.ts", "./src/journeys/setup.ts"],
    globalSetup: ["./journeys.global.js"],
    testTimeout: 240_000,
    hookTimeout: 60_000,
  },
});
