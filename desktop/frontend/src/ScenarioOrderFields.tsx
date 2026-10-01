import type { ScenarioOrder, ScenarioResult, ScenarioStep } from "./bindings";

/** The saved order identifiers and repeated synthetic observations which
 * the Go order/result encoder carries. Every untouched fact stays retained. */
export function ScenarioOrderFields({ orders, results, steps, onOrders, onResults }: { orders: ScenarioOrder[]; results: ScenarioResult[]; steps: ScenarioStep[]; onOrders: (orders: ScenarioOrder[]) => void; onResults: (results: ScenarioResult[]) => void }) {
  return <>
    {orders.map((order, at) => <fieldset key={order.subject}><legend>Order {order.subject}</legend>
      {(["placer", "filler"] as const).map((side) => (["namespace", "identifier"] as const).map((key) => <label key={`${side}-${key}`}>{`${side === "placer" ? "Placer" : "Filler"} ${key}`}<input type="text" value={order[side][key]} onChange={(event) => onOrders(orders.map((held, i) => i === at ? { ...held, [side]: { ...held[side], [key]: event.target.value } } : held))} /></label>))}
    </fieldset>)}
    {results.map((result, at) => <fieldset key={at}><legend>Result {result.step}</legend>
      <label>Result step<select value={result.step} onChange={(event) => onResults(results.map((held, i) => i === at ? { ...held, step: event.target.value } : held))}>{steps.some((step) => step.id === result.step) ? null : <option>{result.step}</option>}{steps.map((step) => <option key={step.id}>{step.id}</option>)}</select></label>
      {result.observations.map((observation, index) => <fieldset key={index}><legend>Observation {index + 1}</legend>{(["code", "sub_id", "value", "status"] as const).map((key) => <label key={key}>{`Observation ${key === "sub_id" ? "sub-ID" : key} ${index + 1}`}<input type="text" value={observation[key]} onChange={(event) => onResults(results.map((held, i) => i === at ? { ...held, observations: held.observations.map((old, j) => j === index ? { ...old, [key]: event.target.value } : old) } : held))} /></label>)}<button type="button" onClick={() => onResults(results.map((held, i) => i === at ? { ...held, observations: held.observations.filter((_, j) => j !== index) } : held))}>Remove observation {index + 1}</button></fieldset>)}
      <button type="button" onClick={() => onResults(results.map((held, i) => i === at ? { ...held, observations: [...held.observations, { code: "", sub_id: "", value: "", status: "" }] } : held))}>Add observation</button>
    </fieldset>)}
  </>;
}
