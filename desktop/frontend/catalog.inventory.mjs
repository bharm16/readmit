// Enumerate production components with the TypeScript parser, not file counts.
import ts from "typescript";
import { readdirSync, readFileSync, writeFileSync } from "node:fs";
const components = [];
for (const file of readdirSync("src").filter(
  (f) => f.endsWith(".tsx") && !f.endsWith(".test.tsx") && f !== "main.tsx",
)) {
  const source = ts.createSourceFile(
    file,
    readFileSync(`src/${file}`, "utf8"),
    ts.ScriptTarget.Latest,
    true,
    ts.ScriptKind.TSX,
  );
  for (const node of source.statements) {
    if (
      !ts.isFunctionDeclaration(node) ||
      !node.name ||
      !/^[A-Z]/.test(node.name.text)
    )
      continue;
    const exported =
      node.modifiers?.some((m) => m.kind === ts.SyntaxKind.ExportKeyword) ??
      false;
    let jsx = false;
    function visit(n) {
      if (
        ts.isJsxElement(n) ||
        ts.isJsxSelfClosingElement(n) ||
        ts.isJsxFragment(n)
      )
        jsx = true;
      ts.forEachChild(n, visit);
    }
    visit(node);
    if (jsx)
      components.push({
        name: node.name.text,
        source: `desktop/frontend/src/${file}`,
        exported,
        line: source.getLineAndCharacterOfPosition(node.getStart()).line + 1,
      });
  }
}
writeFileSync(
  "src/catalog/inventory.json",
  JSON.stringify(components, null, 2) + "\n",
);
console.log(
  `${components.filter((c) => c.exported).length} exported components; ${components.length} including private components`,
);
