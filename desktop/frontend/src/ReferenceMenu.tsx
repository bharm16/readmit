import { useEffect, useState } from "react";
import { Menu } from "./layout";
import { readReferenceLibrary, type ReferenceEdition } from "./bindings";
import down from "./assets/workbench/down.svg";

export type ReferenceChoice = { entry: ReferenceEdition; serial: number };

export function ReferenceMenu({ edition, disabled, onAutomatic, onChoose, onLibrary, onSelect }: {
  edition: string; disabled: boolean; onAutomatic: () => void; onChoose: () => void; onLibrary: () => void; onSelect: (entry: ReferenceEdition) => void;
}) {
  const [editions, setEditions] = useState<ReferenceEdition[]>([]);
  const [loading, setLoading] = useState(true);
  useEffect(() => {
    if (disabled) return;
    let active = true;
    setLoading(true);
    void readReferenceLibrary().then(answer => {
      if (!active) return;
      setEditions(answer.state === "completed" ? answer.editions : []);
      setLoading(false);
    });
    return () => { active = false; };
  }, [disabled, edition]);
  return <Menu className="reader-reference-menu" label="HL7 version"
    trigger={<>HL7 {edition || "not declared"}<img className="workbench-icon" src={down} alt="" /></>}
    items={[
      ...editions.map(entry => ({ id: `edition-${entry.edition}`, label: entry.edition, current: entry.edition === edition, onSelect: () => onSelect(entry), disabled: disabled || loading || !entry.path })),
      ...(editions.length ? [] : [{ id: "availability", label: loading ? "Loading versions…" : "Definitions unavailable", onSelect: () => undefined, disabled: true }]),
      { id: "automatic", label: "Use message version", onSelect: onAutomatic, disabled, separated: true },
      { id: "custom", label: "Use custom definitions…", onSelect: onChoose, disabled, separated: true },
      { id: "manage", label: "Manage custom definitions…", onSelect: onLibrary, disabled },
    ]} />;
}
