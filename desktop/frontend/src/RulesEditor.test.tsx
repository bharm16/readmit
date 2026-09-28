// The structured document editors, pinned over the typed facade boundary:
// which exact JSON a person's controls compose, that the strict Go parsers see
// that exact text, and that a refusal is shown verbatim and leaves the work in
// the window. What a document means is the Go readers' subject, not this one.
import { expect, test } from "vitest";
import type { ReactElement } from "react";
import { render as renderAlone, screen } from "@testing-library/react";
import { vocabularyWrapper } from "./testkit/app";
import { WORKSPACE_ROOT } from "./testkit/fixtures";
import userEvent from "@testing-library/user-event";
import {
  NormalizationPolicyEditor,
} from "./RulesEditor";
import { installFacade } from "./testkit/wails";

/** A panel on its own, inside the vocabulary the window provides it. */
const render = (ui: ReactElement) => renderAlone(ui, { wrapper: vocabularyWrapper() });

test("a normalization policy rule is one typed operator over one selector", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    SaveNormalizationPolicy: (request) => ({
      state: "completed" as const,
      output: request.output,
      sha256: "policy-sha256-fixed-for-tests",
      document: request.document,
    }),
  });
  render(<NormalizationPolicyEditor workspace={WORKSPACE_ROOT} entries={[]} busy={false} />);
  await user.type(screen.getByLabelText("Policy rule ID"), "sending-time");
  await user.type(screen.getByLabelText("Canonical selector"), "MSH-7");
  await user.selectOptions(screen.getByLabelText("Operator"), "timestamp");
  await user.type(screen.getByLabelText("Precision"), "minute");
  await user.click(screen.getByRole("button", { name: "Add policy rule" }));
  await user.type(screen.getByLabelText("New normalization-policy entry"), "policy-1.json");
  await user.click(screen.getByRole("button", { name: "Save as new" }));
  const [request] = facade.oneCall("SaveNormalizationPolicy");
  expect(JSON.parse(request.document)).toEqual({
    schema: "readmit-normalization-policy/v1",
    rules: [{ id: "sending-time", selector: "MSH-7", operator: "timestamp", precision: "minute" }],
  });
});
