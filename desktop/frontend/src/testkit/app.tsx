// Renders the real application against the stubbed facade, the way main.tsx
// mounts it — StrictMode included — with the quiet reads the window performs
// on start answered by default. A test replaces any of them by naming the
// handler; anything it neither names nor drives stays unanswered and rejects,
// so a journey cannot pass on a call the test never arranged.
import { StrictMode } from "react";
import { render, screen } from "@testing-library/react";
import App from "../App";
import { installFacade } from "./wails";
import type { FacadeHandlers } from "./wails";
import {
  filtersResult,
  guideResult,
  recentResult,
  sessionStored,
  shellResult,
} from "./fixtures";

export async function renderApp(handlers: FacadeHandlers = {}) {
  const facade = installFacade({
    // The window reads these on start; the journeys below replace the ones
    // they are about.
    Shell: () => shellResult(),
    RecentWorkspaces: () => recentResult([]),
    Filters: () => filtersResult(),
    RecoverSession: () => ({ state: "empty" }),
    EditorDrafts: () => ({ state: "empty" }),
    OperationStatus: () => ({ state: "empty", selected: false }),
    // Retaining where the viewer is and dropping a stored draft answer
    // quietly unless a test is about them.
    RecordView: () => sessionStored,
    SaveDraft: () => sessionStored,
    DiscardDraft: () => sessionStored,
    SaveEditorDraft: () => ({ state: "completed" }),
    DiscardEditorDraft: () => ({ state: "completed" }),
    Cancel: async () => {},
    // Opening a folder re-reads the guided sample out of it.
    Guide: () => guideResult("sample", 0),
    ...handlers,
  });
  render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
  // The window is up once it draws the facade's own privacy statement.
  await screen.findByText(shellResult().shell?.privacy.statement ?? "");
  return { facade, screen };
}
