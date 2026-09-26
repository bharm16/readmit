// The two file reads the stylesheet contract test makes, typed without adding
// the whole of Node's declarations to a browser project. Vitest runs its tests
// in Node, so these are the real functions.
declare module "node:fs" {
  export function readdirSync(path: string): string[];
  export function readFileSync(path: string, encoding: "utf8"): string;
}

declare const process: { cwd(): string };
