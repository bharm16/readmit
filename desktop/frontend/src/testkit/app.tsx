// Renders the real application against the stubbed facade, the way main.tsx
// mounts it — StrictMode included — with the quiet reads the window performs
// on start answered by default. A test replaces any of them by naming the
// handler; anything it neither names nor drives stays unanswered and rejects,
// so a journey cannot pass on a call the test never arranged.
import { StrictMode, type ReactNode } from "react";
import { render, screen } from "@testing-library/react";
import App from "../App";
import { installFacade } from "./wails";
import type { FacadeHandlers } from "./wails";
import type { Vocabulary } from "../bindings";
import { VocabularyContext } from "../vocabulary";
import {
  catalogOfListing,
  filtersResult,
  messagesResult,
  sessionStored,
  shellResult,
  vocabularyFixture,
} from "./fixtures";

/** A wrapper for a panel rendered on its own: the vocabulary the facade
 * publishes in the window's description, provided as the window provides it. */
export function vocabularyWrapper(vocabulary: Vocabulary = vocabularyFixture()) {
  return function WithVocabulary({ children }: { children: ReactNode }) {
    return <VocabularyContext.Provider value={vocabulary}>{children}</VocabularyContext.Provider>;
  };
}

export async function renderApp(handlers: FacadeHandlers = {}) {
  const facade = installFacade({
    // The window reads these on start; the journeys below replace the ones
    // they are about.
    Shell: () => shellResult(),
    Filters: () => filtersResult(),
    EditorDrafts: () => ({ state: "empty" }),
    OperationStatus: () => ({ state: "empty", selected: false, author_seats: 0, runner_instances: 0 }),
    CommercialStatus: () => ({ state: "empty" }),
    LicenseStatus: () => ({ state: "empty" }),
    HubStatus: () => ({ state: "empty", connected: false, authenticated: false }),
    ReadPreferences: () => ({ state: "completed", preferences: { theme: "system", text_scale: 100 } }),
    ListConnections: (context) => ({ state: "empty", context, rows: [] }),
    // Retaining where the viewer is and dropping a stored draft answer
    // quietly unless a test is about them.
    RecordView: () => sessionStored,
    SaveEditorDraft: () => ({ state: "completed" }),
    DiscardEditorDraft: () => ({ state: "completed" }),
    Cancel: async () => {},
    // Opening a case reads its messages and the project's saved views.
    ReadMessages: () => messagesResult([]),
    ListViews: () => ({ state: "empty", views: [] }),
    // Opening a folder reads whether it is the demo; an ordinary project is not.
    DemoProgress: () => ({ state: "empty", reason: "this project is not the demo" }),
    ...handlers,
  });
  // The catalog lists the cases of the folder the window last opened, the way
  // the facade reads them from the same listing, unless a test arranges its
  // own answer.
  if (!handlers.ListCatalog) {
    facade.reply({ ListCatalog: (query) => catalogOfListing(query, facade) });
  }
  render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
  // The window is up once it draws its navigation, which it does only after
  // the facade described the shell.
  await screen.findByRole("navigation", { name: "Main" });
  return { facade, screen };
}
