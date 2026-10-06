import { Menu } from "./layout";
import down from "./assets/workbench/down.svg";

export function ReferenceMenu({ edition, disabled, onAutomatic, onChoose, onLibrary }: {
  edition: string; disabled: boolean; onAutomatic: () => void; onChoose: () => void; onLibrary: () => void;
}) {
  return <Menu className="reader-reference-menu" label="HL7 reference version"
    trigger={<>HL7 {edition || "not declared"}<img className="workbench-icon" src={down} alt="" /></>}
    items={[
      { label: "Use message version", onSelect: onAutomatic, disabled },
      { label: "Choose reference version…", onSelect: onLibrary, disabled },
      { label: "Use local catalog…", onSelect: onChoose, disabled, separated: true },
      { label: "Manage reference library…", onSelect: onLibrary, disabled },
    ]} />;
}
