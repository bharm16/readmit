import type { TargetClassification } from "./bindings";
import { TARGET_CLASSIFICATIONS } from "./display";
import "./environment.css";

/** The environment a run, replay or test addresses, as those flows show it:
 * its name, classification and address, and — only when sending to it is
 * refused — that one consequence. */
export function EnvironmentBanner({
  name,
  classification = "unclassified",
  address,
}: {
  name?: string | undefined;
  classification?: TargetClassification | string | undefined;
  address?: string | undefined;
}) {
  const code = (classification || "unclassified") as TargetClassification;
  const shown = TARGET_CLASSIFICATIONS[code] ?? "Not classified";
  const refused = code !== "nonproduction";
  return (
    <div className="environment-banner" role="status" aria-label="Target environment identity">
      <div className="environment-banner-header">
        <span className="environment-banner-title">{name || "No environment chosen"}</span>
        <span className="environment-classification">{shown}</span>
        {address ? <span className="environment-peer">{address}</span> : null}
      </div>
      {refused ? <p className="consequence">Sending is refused for {shown === "Not classified" ? "an unclassified" : "a production"} environment.</p> : null}
    </div>
  );
}
