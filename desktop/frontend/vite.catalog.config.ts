import { defineConfig } from "vite";
import { resolve } from "node:path";
import { readFileSync } from "node:fs";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [
    {
      name: "catalog-component-identities",
      enforce: "pre",
      resolveId(id) {
        if (id === "virtual:catalog-private") return "\0catalog-private";
      },
      load(id) {
        if (id !== "\0catalog-private") return;
        const items: { name: string; source: string; exported: boolean }[] =
          JSON.parse(readFileSync("src/catalog/inventory.json", "utf8"));
        const privateItems = items.filter((item) => !item.exported);
        return (
          privateItems
            .map(
              (item, i) =>
                `import { __catalog_${item.name} as C${i} } from ${JSON.stringify(resolve(item.source.replace("desktop/frontend/", "")))};`,
            )
            .join("\n") +
          "\nexport default {" +
          privateItems
            .map(
              (item, i) =>
                `${JSON.stringify(item.source + "#" + item.name)}: C${i}`,
            )
            .join(",") +
          "};"
        );
      },
      transform(code, id) {
        const items: { name: string; source: string; exported: boolean }[] =
          JSON.parse(readFileSync("src/catalog/inventory.json", "utf8"));
        const components = items.filter((item) =>
          id.endsWith(item.source.replace("desktop/frontend/", "")),
        );
        if (!components.length) return;
        return (
          code +
          "\n" +
          components
            .map(
              (item) =>
                `${item.name}.displayName = ${JSON.stringify(item.source + "#" + item.name)};`,
            )
            .join("\n") +
          "\n" +
          components
            .filter((item) => !item.exported)
            .map((item) => `export { ${item.name} as __catalog_${item.name} };`)
            .join("\n")
        );
      },
    },
    react(),
  ],
  build: {
    outDir: "../captureapp/dist",
    emptyOutDir: true,
    minify: false,
    rollupOptions: { input: "catalog.html" },
  },
});
