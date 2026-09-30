// The filesystem reads used by contract and exported-evidence tests, typed
// without adding all Node declarations to the browser project. Vitest uses
// these real functions only to inspect its own temporary evidence roots.
declare module "node:fs" {
  export function existsSync(path: string): boolean;
  export function readdirSync(path: string, options: { recursive: true; withFileTypes: true }): { isFile(): boolean; parentPath: string; name: string }[];
  export function readdirSync(path: string): string[];
  export function readFileSync(path: string, encoding: "utf8"): string;
}

declare const process: { cwd(): string };

declare module "node:path" {
  export function join(...paths: string[]): string;
}
