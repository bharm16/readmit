import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The desktop shell is offline software: everything it renders is bundled into
// frontend/dist and embedded in the executable. Nothing is fetched at runtime.
export default defineConfig({
  plugins: [react()],
  build: { outDir: "dist", emptyOutDir: true, assetsInlineLimit: 0 },
});
