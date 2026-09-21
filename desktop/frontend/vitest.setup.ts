// Independent reset between tests: the previous test's window, its answers
// and its stubbed boundary are gone before the next one starts, so no result
// one test arranged is visible to another.
import { afterEach } from "vitest";
import { cleanup } from "@testing-library/react";
import { uninstallFacade } from "./src/testkit/wails";

afterEach(() => {
  cleanup();
  uninstallFacade();
});

// jsdom models the dialog element but not the showModal/close entry points a
// browser provides, and the command palette is a native modal dialog on
// purpose. This shim provides exactly those two entry points and nothing
// else: no focus trapping and no key handling, which stay the platform's and
// are not what these tests claim.
if (
  typeof HTMLDialogElement !== "undefined" &&
  !HTMLDialogElement.prototype.showModal
) {
  HTMLDialogElement.prototype.showModal = function showModal() {
    this.open = true;
  };
  HTMLDialogElement.prototype.close = function close() {
    if (this.open) {
      this.open = false;
      this.dispatchEvent(new Event("close"));
    }
  };
}
