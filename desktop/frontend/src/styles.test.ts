// The stylesheets' own contract: every colour is a system colour or mixed from
// them, nothing is fetched, and a panel's stylesheet never gives controls or
// table cells a geometry of their own. The shared tokens in styles.css are the
// only place sizes are decided.
import { readdirSync, readFileSync } from "node:fs";
import { expect, test } from "vitest";
import * as geometry from "./geometry";

// Every stylesheet beside the components, as its text.
const here = `${process.cwd()}/src`;
const sheets = readdirSync(here)
  .filter((name) => name.endsWith(".css"))
  .map((name) => ({ name, css: readFileSync(`${here}/${name}`, "utf8").replace(/\/\*[\s\S]*?\*\//g, "") }));

type Rule = { sheet: string; selector: string; property: string; value: string };
function rules(): Rule[] {
  const found: Rule[] = [];
  for (const { name, css } of sheets) {
    for (const block of css.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
      const selector = block[1]!.trim();
      for (const declaration of block[2]!.split(";")) {
        const at = declaration.indexOf(":");
        if (at < 0) continue;
        found.push({ sheet: name, selector, property: declaration.slice(0, at).trim().toLowerCase(), value: declaration.slice(at + 1).trim() });
      }
    }
  }
  return found;
}

const NAMED = /(?<![-\w])(red|green|blue|white|black|orange|darkorange|gray|grey|silver|teal|navy|yellow|purple|pink|brown|maroon|olive|lime|aqua|cyan|magenta|fuchsia|gold|beige|ivory|coral|salmon|crimson|indigo|violet|tan|khaki)\b/i;
const COLOURED = /^(color|background|background-color|border|border-(top|right|bottom|left)(-color)?|border-color|outline|outline-color|box-shadow|text-shadow|fill|stroke|caret-color|accent-color|text-decoration-color|column-rule|--.*)$/;

test("the stylesheets are found", () => {
  expect(sheets.map((sheet) => sheet.name)).toContain("styles.css");
  expect(sheets.every((sheet) => sheet.css.length > 0)).toBe(true);
});

test("no colour literal, gradient or fixed shadow colour in any first-party stylesheet", () => {
  const offending = rules().filter(
    (rule) =>
      /#[0-9a-f]{3,8}\b/i.test(rule.value) ||
      /\b(rgb|rgba|hsl|hsla|hwb|lab|lch|oklab|oklch)\(/i.test(rule.value) ||
      /gradient\(/i.test(rule.value) ||
      (COLOURED.test(rule.property) && NAMED.test(rule.value)),
  );
  expect(offending).toEqual([]);
});

test("nothing is fetched and forced colours are never opted out of", () => {
  for (const { name, css } of sheets) {
    expect(css, name).not.toMatch(/@import|url\(\s*["']?(https?:|\/\/)/i);
    expect(css, name).not.toMatch(/forced-color-adjust\s*:\s*none/i);
  }
});

test("a panel's stylesheet does not size controls or table cells itself", () => {
  const controls = /(^|[\s,>+~(])(button|input|select|textarea|th|td)\b/;
  const geometry = /^(height|min-height|max-height|padding(-\w+)?|font-size|line-height|border-radius)$/;
  const offending = rules().filter(
    (rule) =>
      rule.sheet !== "styles.css" &&
      controls.test(rule.selector) &&
      geometry.test(rule.property) &&
      // A text area's height is how much it shows, not its control geometry.
      !(/\btextarea\b/.test(rule.selector) && !/\b(button|input|select|th|td)\b/.test(rule.selector) && /height$/.test(rule.property)),
  );
  expect(offending.map((rule) => `${rule.sheet}: ${rule.selector} { ${rule.property} }`)).toEqual([]);
});

test("the layout lengths the code decides with are the stylesheet's own tokens", () => {
  const root = sheets.find((sheet) => sheet.name === "styles.css")!.css;
  const rem = (name: string) => Number.parseFloat(root.match(new RegExp(`${name}\\s*:\\s*([0-9.]+)rem;`))?.[1] ?? "NaN");
  expect(rem("--sidebar")).toBe(geometry.SIDEBAR_REM);
  expect(rem("--icon-rail")).toBe(geometry.ICON_RAIL_REM);
  expect(rem("--row")).toBe(geometry.ROW_REM);
  expect(rem("--inspector")).toBe(geometry.INSPECTOR_REM);
  // The categories' rail and the icon rail's breakpoint are only decided in
  // code; the stylesheet draws whichever the code chose.
  expect(root).toContain(".categories.narrow");
  expect(root).toContain(".app.compact");
});

test("a selected or current item keeps a marker when the platform forces its colours", () => {
  const root = sheets.find((sheet) => sheet.name === "styles.css")!.css;
  const forced = root.slice(root.indexOf("@media (forced-colors: active)"));
  for (const selected of ['.nav-item[aria-current="page"]', '.table-view tbody tr[aria-selected="true"]', '.palette-results > li[aria-selected="true"]']) {
    expect(forced).toContain(selected);
  }
  expect(forced).toMatch(/outline:\s*2px solid Highlight/);
});

test("the shared tokens hold the specified geometry", () => {
  const root = sheets.find((sheet) => sheet.name === "styles.css")!.css;
  const token = (name: string) => root.match(new RegExp(`${name}\\s*:\\s*([^;]+);`))?.[1]?.trim();
  expect(token("--sidebar")).toBe("13rem");
  expect(token("--icon-rail")).toBe("3.25rem");
  expect(token("--header")).toBe("3.5rem");
  expect(token("--tabs")).toBe("2.25rem");
  expect(token("--toolbar")).toBe("2.75rem");
  expect(token("--row")).toBe("2.75rem");
  expect(token("--button")).toBe("2rem");
  expect(token("--input")).toBe("2.25rem");
  expect(token("--radius-control")).toBe("0.25rem");
  expect(token("--radius-sheet")).toBe("0.5rem");
  expect(token("--sheet-small")).toBe("30rem");
  expect(token("--sheet-normal")).toBe("35rem");
  expect(token("--sheet-wide")).toBe("45rem");
  expect(token("--report-column")).toBe("47.5rem");
  expect(token("--line")).toBe("color-mix(in srgb, CanvasText 16%, Canvas)");
  expect(token("--selection")).toBe("color-mix(in srgb, var(--accent) 12%, Canvas)");
});
