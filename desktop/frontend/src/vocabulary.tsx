import { createContext, useContext } from "react";
import type { Vocabulary } from "./bindings";

/** What the window offers a person to choose among and the bounds it pages
 * by, as the facade publishes them in the window's description. A panel reads
 * them here and keeps no copy of its own, so a choice it offers is always one
 * the Go side accepts. It is null until the description has been read. */
export const VocabularyContext = createContext<Vocabulary | null>(null);

export function useVocabulary(): Vocabulary | null {
  return useContext(VocabularyContext);
}
