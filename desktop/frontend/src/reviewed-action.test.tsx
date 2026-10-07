import { useState } from "react";
import { test, expect, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SendReview } from "./RunPanel";
import { ReviewSheet } from "./ReviewSheet";
import { installFacade } from "./testkit/wails";
import type { ActionReviewResult, PrepareActionRequest } from "./bindings";

test("a reviewed action keeps the current visible choice when an earlier preparation arrives late", async () => {
  const pending: { request: PrepareActionRequest; resolve: (answer: ActionReviewResult) => void }[] = [];
  const facade = installFacade({
    PrepareAction: (request) => new Promise<ActionReviewResult>(resolve => pending.push({request, resolve})),
    ExecuteReviewedAction: request => ({state:"completed", context:request.context, outcome:"completed", replayed:false}),
    WithdrawReview: () => ({state:"completed", context:{project:"",generation:0}})
  });
  const context = () => ({project:"/audit/project",generation:0});
  function Harness() {
    const [source,setSource] = useState("A");
    return <ReviewSheet open title="Review race" action="team.upload" finalLabel="Upload" context={context} items={[]} options={{team:{project:"team-project",source}}} prepareKey={source} onClose={() => undefined} onDone={() => undefined} fields={<label>Source<input value={source} onChange={event=>setSource(event.target.value)} /></label>} render={review=><p>Review token: {review.token}</p>} />;
  }
  render(<Harness/>);
  await waitFor(()=>expect(pending.length).toBe(1));
  const user=userEvent.setup();
  await user.clear(screen.getByRole("textbox",{name:"Source"}));
  await user.type(screen.getByRole("textbox",{name:"Source"}),"B");
  await waitFor(()=>expect(pending.length).toBe(2));
  const answer=(at:number,token:string):ActionReviewResult=>({state:"completed",context:pending[at]!.request.context,review:{action:"team.upload",token,ready:true,requirements:[],consent:"upload",items:[],destination:{name:"Team"}}});
  pending[1]!.resolve(answer(1,"token-B"));
  expect(await screen.findByText("Review token: token-B")).toBeTruthy();
  pending[0]!.resolve(answer(0,"token-A"));
  await waitFor(() => expect(facade.callsTo("WithdrawReview")).toHaveLength(1));
  expect(screen.getByText("Review token: token-B")).toBeTruthy();
  expect((screen.getByRole("textbox",{name:"Source"}) as HTMLInputElement).value).toBe("B");
  await user.click(screen.getByRole("button",{name:"Upload"}));
  await waitFor(()=>expect(facade.callsTo("ExecuteReviewedAction").length).toBe(1));
  expect(facade.oneCall("ExecuteReviewedAction")[0].token).toBe("token-B");
  expect(facade.oneCall("WithdrawReview")[0]).toBe("token-A");
});

test("closing a review withdraws an answer that completes after close", async () => {
  let answer: ((value: ActionReviewResult) => void) | undefined;
  let requested: PrepareActionRequest | undefined;
  const facade = installFacade({
    PrepareAction: request => new Promise<ActionReviewResult>(resolve => { requested = request; answer = resolve; }),
    WithdrawReview: () => ({state:"completed", context:{project:"",generation:0}}),
  });
  const props = {title:"Close review", action:"team.upload" as const, finalLabel:"Upload", context:() => ({project:"/audit/project",generation:0}), items:[], onClose:() => undefined, onDone:() => undefined, render:() => <p>Reviewed</p>};
  const shown = render(<ReviewSheet {...props} open />);
  await waitFor(() => expect(requested).toBeTruthy());
  shown.rerender(<ReviewSheet {...props} open={false} />);
  answer!({state:"completed", context:requested!.context, review:{action:"team.upload", consent:"upload", items:[], destination:{name:"Team"}, token:"closed-token", ready:true, requirements:[]}});
  await waitFor(() => expect(facade.oneCall("WithdrawReview")[0]).toBe("closed-token"));
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
  expect(screen.queryByText("Reviewed")).toBeNull();
});


test("a send held before dispatch cannot spend a review after its visible choice changes", async () => {
  const waits: (() => void)[] = [];
  const started = vi.fn();
  const facade = installFacade({
    ListCatalog: request => ({ state: "completed", context: request.context, page: { items: [], total: 0, snapshot: "s", incomplete: [], recorded: true } }),
    PrepareAction: request => ({ state: "completed", context: request.context, review: {
      action: "run.test", token: "token-" + request.items[0]!.id, ready: true, requirements: [], consent: "send", items: [], destination: { name: "Fixture", output: "job-" + request.items[0]!.id },
    } }),
    WithdrawReview: () => ({ state: "completed", context: { project: "", generation: 0 } }),
    ExecuteReviewedAction: request => ({ state: "completed", context: request.context, outcome: "completed", replayed: false }),
  });
  function Harness() {
    const [test, setTest] = useState("A");
    return <><button onClick={() => setTest("B")}>Choose B</button><SendReview request={{ kind: "test", test: { kind: "test", id: test } }} context={() => ({ project: "/audit/project", generation: 1 })} onClose={() => undefined} onStarted={started} onBeforeSend={() => new Promise<void>(resolve => waits.push(resolve))} onEditEnvironment={() => undefined} onActivate={() => undefined} /></>;
  }
  render(<Harness />);
  const user = userEvent.setup();
  await waitFor(() => expect((screen.getByRole("button", { name: "Send" }) as HTMLButtonElement).disabled).toBe(false));
  await user.click(screen.getByRole("button", { name: "Send" }));
  await waitFor(() => expect(waits).toHaveLength(1));
  await user.click(screen.getByRole("button", { name: "Choose B" }));
  await waitFor(() => expect(facade.callsTo("PrepareAction")).toHaveLength(2));
  waits[0]!();
  expect(await screen.findByText("What this review covered changed. Review it again.")).toBeTruthy();
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
  expect(started).not.toHaveBeenCalled();
  expect(facade.oneCall("WithdrawReview")[0]).toBe("token-A");
  await user.click(screen.getByRole("button", { name: "Send" }));
  await waitFor(() => expect(waits).toHaveLength(2));
  waits[1]!();
  await waitFor(() => expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(1));
  expect(facade.oneCall("ExecuteReviewedAction")[0].token).toBe("token-B");
  expect(started).toHaveBeenCalledOnce();
});

test("editing target setup carries only window-owned choices and reopens a fresh unconfirmed review",async()=>{
 let returned:import("./RunPanel").SendRequest|undefined;
 const facade=installFacade({
 ListCatalog:request=>({state:"completed",context:request.context,page:{items:[],total:0,snapshot:"s",recorded:true,incomplete:[]}}),
 PrepareAction:request=>({state:"completed",context:request.context,review:{action:"run.test",token:`fresh-${request.context.generation}`,ready:false,refusal:"Target setup is incomplete",consent:"send",items:[],requirements:[],destination:{name:"Owned target"},run:{kind:"test",name:"Owned test",message_count:1,environments:[],refusal:"environment",environment:{kind:"environment",id:"owned-env",revision:"1"},targets:[],jobs:[],resets:[],setup:[],messages:[]}}}),
 WithdrawReview:()=>({state:"completed",context:{project:"",generation:0}}),
 });
 const request:import("./RunPanel").SendRequest={kind:"test",test:{kind:"test",id:"owned-test"}};
 const props={context:()=>({project:"/owned/project",generation:1}),onClose:()=>undefined,onStarted:()=>undefined,onEditEnvironment:(_environment:import("./bindings").ItemRef,next:import("./RunPanel").SendRequest)=>{returned=next;},onActivate:()=>undefined};
 const rendered=render(<SendReview {...props} request={request}/>);const user=userEvent.setup();
 await user.click(await screen.findByRole("button",{name:"Edit environment"}));
 expect(returned).toMatchObject({kind:"test",environment:{id:"owned-env"},resumeChoices:{transformations:[]}});
 expect(JSON.stringify(returned)).not.toContain('"token"');expect(JSON.stringify(returned)).not.toContain('"confirmed"');
 rendered.rerender(<SendReview {...props} request={null}/>);rendered.rerender(<SendReview {...props} request={returned!}/>);
 await waitFor(()=>expect(facade.callsTo("PrepareAction").length).toBeGreaterThan(1));
 expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test("connected suite dispatch shows every job and editing installed authority withdraws the ready review",async()=>{
 const facade=installFacade({PrepareAction:request=>({state:"completed",context:request.context,review:{action:"run.suite",ready:!!request.run?.connected,token:request.run?.connected?"installed-review":"",refusal:request.run?.connected?"":"Select installed authority",requirements:[],consent:"send",items:[],destination:{name:"Owned runner"},run:{kind:"suite",name:"Connected suite",message_count:null,messages:[],setup:[],resets:[],jobs:[],targets:[],environments:[],connected:{input:"prepared",project:"project",environment:"qa",capabilities:{schema:"capabilities",engine:"engine",pins:[]},jobs:[{id:"ready-job",state:"enabled",plan_identity:"p",input:"i",after:[]},{id:"refused-job",state:"refused",plan_identity:"p2",input:"i2",after:[]},{id:"skipped-job",state:"skipped",plan_identity:"p3",input:"i3",after:[]}]}}}}),WithdrawReview:()=>({state:"completed",context:{project:"",generation:0}})});
 render(<SendReview request={{kind:"suite",suite:{kind:"suite",id:"owned"},environment:"qa",connectedRequired:true}} context={()=>({project:"/owned",generation:1})} onClose={()=>{}} onStarted={()=>{}} onEditEnvironment={()=>{}} onActivate={()=>{}}/>);
 const user=userEvent.setup();for(const [label,value] of [["Runner configuration","runner.json"],["Installed authority","authority.json"],["Approved promotion","promotion.json"],["Promotion SHA-256","a".repeat(64)],["Target revision","r1"],["Dispatch identity","dispatch-one"]])await user.type(screen.getByLabelText(label!),value!);
 await user.click(screen.getByRole("button",{name:"Review runner dispatch"}));await waitFor(()=>expect((screen.getByRole("button",{name:"Send"}) as HTMLButtonElement).disabled).toBe(false));
 expect(screen.getByRole("region",{name:"Connected suite jobs"}).querySelectorAll("tbody tr")).toHaveLength(3);expect(screen.getByText("refused-job")).toBeTruthy();expect(screen.getByText("skipped-job")).toBeTruthy();
 await user.type(screen.getByLabelText("Installed authority"),"-changed");await waitFor(()=>expect((screen.getByRole("button",{name:"Send"}) as HTMLButtonElement).disabled).toBe(true));
 await waitFor(()=>expect(facade.callsTo("WithdrawReview").some(call=>call.args[0]==="installed-review")).toBe(true));
 expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test.each([
  ["mtls", "MLLP · Mutual TLS"],
  ["tls", "MLLP · TLS"],
  ["plain", "MLLP · Plain"],
])("connected Run review shows its compiled %s transport before consent", async (transport, label) => {
  const facade = installFacade({
    ListCatalog: request => ({ state: "completed", context: request.context, page: { items: [], total: 0, snapshot: "s", recorded: true, incomplete: [] } }),
    PrepareAction: request => ({ state: "completed", context: request.context, review: {
      action: "run.test", token: "transport-review", ready: true, requirements: [], consent: "send", items: [], destination: { name: "Receiver" },
      run: { kind: "test", name: "Saved test", environment_name: "Receiver", environment: {kind: "environment", id: "receiver"}, message_count: 2, environments: [], targets: [], jobs: [], resets: [], setup: [], messages: [],
        lifecycle: { transport, plan: "plan", input: "input", instance: "instance", boundary: "application-state", phases: [], effects: [], endpoints: [] } },
    } }),
    WithdrawReview: () => ({state:"completed", context:{project:"",generation:0}}),
  });
  render(<SendReview request={{ kind: "test", test: { kind: "test", id: "saved" } }} context={() => ({ project: "/owned", generation: 1 })} onClose={() => {}} onStarted={() => {}} onEditEnvironment={() => {}} onActivate={() => {}} />);
  expect(await screen.findByText(label)).toBeTruthy();
  expect(screen.getByText("Transport")).toBeTruthy();
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});
