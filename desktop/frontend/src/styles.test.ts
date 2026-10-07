// The stylesheets' own contract: colours belong to the approved theme palette
// or system roles, nothing is fetched, and a panel's stylesheet never gives controls or
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

// Approved Figma page 12 previews: 2388:26677 (Light), 2394:3836 (Dark).
// Hover and disabled roles use the same neutral scale. Page 14 adds the
// existing Readmit/Sheet shadow colour for contextual reference cards.
const FIGMA_PALETTES: Record<string, Record<string, string>> = {
  light: {
    "--reference-hover-shadow-color": "#0000002e",
    "--syntax-type": "#751ed9", "--syntax-date": "#008809", "--syntax-code": "#bd5800", "--syntax-number": "#0071ea", "--syntax-punctuation": "#666666",
    "--canvas": "#ffffff", "--text": "#1a1c1f", "--muted": "#5d5d5d",
    "--field": "#ffffff", "--rail": "#f9f9f9", "--line": "#e4e4e4",
    "--selection": "#ededed", "--selection-border": "#afafaf", "--link": "#1a1c1f",
    "--hover": "#f3f3f3", "--interface-color-action-hover": "#303030",
    "--interface-color-border": "#cdcdcd", "--interface-color-on-action": "#ffffff",
  },
  dark: {
    "--reference-hover-shadow-color": "#0000002e",
    "--syntax-type": "#b06dff", "--syntax-date": "#85df7b", "--syntax-code": "#fa994c", "--syntax-number": "#6dcbf4", "--syntax-punctuation": "#999999",
    "--canvas": "#181818", "--text": "#dfdfdf", "--muted": "#afafaf",
    "--field": "#282828", "--rail": "#212121", "--line": "#343434",
    "--selection": "#303030", "--selection-border": "#5d5d5d", "--link": "#ededed",
    "--hover": "#282828", "--interface-color-action-hover": "#ededed",
    "--interface-color-border": "#414141", "--interface-color-on-action": "#0d0d0d",
  },
};
const THEME_SELECTORS: Record<string, string> = {
  ":root": "light",
  ':root[data-theme="dark"]': "dark",
  ':root:not([data-theme="light"])': "dark",
};
function canonicalPaletteRole(rule: Rule): boolean {
  const palette = FIGMA_PALETTES[THEME_SELECTORS[rule.selector] ?? ""];
  return rule.sheet === "styles.css" && palette?.[rule.property] === rule.value;
}

test("saved and system themes declare the approved Figma palette", () => {
  for (const [selector, theme] of Object.entries(THEME_SELECTORS)) {
    const declared = rules().filter(rule => rule.selector === selector && canonicalPaletteRole(rule));
    expect(declared.map(rule => rule.property).sort()).toEqual(Object.keys(FIGMA_PALETTES[theme]!).sort());
  }
});

test("no colour literal outside the canonical Figma palette, gradient or fixed shadow colour", () => {
  const offending = rules().filter(
    (rule) => !canonicalPaletteRole(rule) && (
      /#[0-9a-f]{3,8}\b/i.test(rule.value) ||
      /\b(rgb|rgba|hsl|hsla|hwb|lab|lch|oklab|oklch)\(/i.test(rule.value) ||
      /gradient\(/i.test(rule.value) ||
      (COLOURED.test(rule.property) && NAMED.test(rule.value))),
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
  expect(rem("--reader-compact-window")).toBe(geometry.READER_COMPACT_WINDOW_REM);
  expect(rem("--reader-compact-sidebar")).toBe(geometry.READER_COMPACT_SIDEBAR_REM);
  expect(rem("--message-browser")).toBe(geometry.MESSAGE_BROWSER_REM);
  expect(rem("--message-browser-compact")).toBe(geometry.MESSAGE_BROWSER_COMPACT_REM);
  expect(rem("--reader-reference")).toBe(geometry.READER_REFERENCE_REM);
  expect(rem("--reader-reference-compact")).toBe(geometry.READER_REFERENCE_COMPACT_REM);
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
  expect(token("--sidebar")).toBe("14rem");
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
});

test("outside the shared tokens, lengths are rem and radii are the shared radii", () => {
  // A physical pixel is kept only for lines: borders, outlines, hairlines and
  // their offsets. Everything else scales with the text through rem.
  const physical = /^(border(-(top|right|bottom|left))?(-width)?|outline(-width|-offset)?|box-shadow|column-rule)$/;
  const offending = rules().filter((rule) => {
    if (rule.sheet === "styles.css" || rule.property.startsWith("--")) return false;
    if (rule.property === "border-radius") return !/^(0|var\(--radius-(control|sheet)\)|50%)$/.test(rule.value);
    if (physical.test(rule.property)) return false;
    // A one-pixel hairline drawn as a box is a line too.
    if (/^(width|height|min-width|min-height)$/.test(rule.property) && rule.value === "1px") return false;
    return /(^|[\s(,])-?\d*\.?\d+px\b/.test(rule.value);
  });
  expect(offending.map((rule) => `${rule.sheet}: ${rule.selector} { ${rule.property}: ${rule.value} }`)).toEqual([]);
});

test("no stylesheet decides a layout by the window's width", () => {
  // A width media query compares against the default text size, so it would
  // ignore a 200% text scale; the code measures the window in rem instead.
  for (const { name, css } of sheets) {
    expect(css, name).not.toMatch(/@media[^{]*\b(min|max)-width/i);
  }
});

test("a control that hides its focus ring shows focus another way", () => {
  const all = rules();
  const hidden = all.filter((rule) => rule.selector.split(",").some((part) => part.trim().endsWith(":focus-visible")) && rule.property === "outline" && /^(none|0)$/.test(rule.value));
  for (const rule of hidden) {
    // Either the same rule fills it, as a menu item or the separator is, or
    // its container draws the ring around it.
    const container = rule.selector.match(/^\.[\w-]+/)?.[0];
    const filled = all.some((other) => other.sheet === rule.sheet && other.selector === rule.selector && /^(background|box-shadow)$/.test(other.property));
    // A landmark region takes focus only to move a person into it; the
    // control there shows focus, the whole pane never does.
    if (/^\.region:focus/.test(rule.selector)) continue;
    const ringed = all.some((other) => other.selector === `${container}:focus-within` && other.property === "outline" && !/^(none|0)$/.test(other.value));
    expect(filled || ringed, rule.selector).toBe(true);
  }
});
