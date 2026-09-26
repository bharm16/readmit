// The capture Vite plugin exposes private components only in its own build.
declare module "virtual:catalog-private" {
  import type { ComponentType } from "react";
  const components: Record<string, ComponentType<Record<string, unknown>>>;
  export default components;
}
