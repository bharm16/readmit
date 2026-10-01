import { test, expect } from "vitest";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { installFacade } from "./testkit/wails";
import { useCallback, useEffect, useState } from "react";
import { useEnvironments } from "./Environments";
import { listWholeCatalog, openItemDraft, type ItemRequest, type ItemDraftResult } from "./bindings";
import { useOwnedRead } from "./ownedRead";
import type { CatalogQuery, CatalogResult, CatalogItem } from "./bindings";
test.each(["completed", "failed"] as const)("an environment list ignores a late %s from the previous project", async (lateState) => {
  const pending: { request: CatalogQuery; resolve: (answer: CatalogResult)=>void }[]=[];
  installFacade({ ListCatalog: request => new Promise<CatalogResult>(resolve=>pending.push({request,resolve})) });
  function Harness(){
    const [root,setRoot]=useState("/audit/project-A");
    const context=useCallback(()=>({project:root,generation:1}),[root]);
    const env=useEnvironments({root,context,place:{kind:"list"},go:()=>undefined,back:()=>undefined,busy:false});
    return <><button onClick={()=>setRoot("/audit/project-B")}>Project B</button><p>Current project: {root}</p>{env.body}</>;
  }
  const item=(name:string):CatalogItem=>({ref:{kind:"environment",id:name,revision:"rev-1"},name,created_at:null,updated_at:null,last_opened_at:null,availability:"available",capabilities:[],summary:{environment:{classification:"nonproduction",address:name,transport:"plain",transport_approved:true,approval_required:true,last_checked_at:null,observation:null,has_policy:false,reset_actions:0}}});
  const answer=(at:number,name:string):CatalogResult=>({state:"completed",context:pending[at]!.request.context,page:{items:[item(name)],total:1,snapshot:"snapshot",incomplete:[],recorded:true}});
  render(<Harness/>);
  await waitFor(()=>expect(pending.length).toBe(1));
  await userEvent.setup().click(screen.getByRole("button",{name:"Project B"}));
  await waitFor(()=>expect(pending.length).toBe(2));
  pending[1]!.resolve(answer(1,"Environment B"));
  expect(await screen.findByText("Environment B",{selector:".case-name span"})).toBeTruthy();
  await act(async () => {
    pending[0]!.resolve(lateState === "completed" ? answer(0,"Environment A") : { state: "failed", context: pending[0]!.request.context, reason: "Old project refused" });
  });
  await waitFor(() => expect(screen.queryByText("Environment A",{selector:".case-name span"})).toBeNull());
  expect(screen.getByText("Environment B",{selector:".case-name span"})).toBeTruthy();
  expect(screen.getByText("Current project: /audit/project-B")).toBeTruthy();
  expect(screen.queryByText("Old project refused")).toBeNull();
});


test("an object read owns its grouped details while an independent read lane remains live", async () => {
  const details: { request: ItemRequest; resolve: (answer: ItemDraftResult) => void }[] = [];
  const lists: { request: CatalogQuery; resolve: (answer: CatalogResult) => void }[] = [];
  installFacade({
    OpenItemDraft: request => new Promise<ItemDraftResult>(resolve => details.push({ request, resolve })),
    ListCatalog: request => new Promise<CatalogResult>(resolve => lists.push({ request, resolve })),
  });
  const context = () => ({ project: "/audit/project", generation: 1 });
  function Harness() {
    const [object, setObject] = useState("A");
    const [detail, setDetail] = useState("Loading object");
    const [independent, setIndependent] = useState("Loading list");
    const readDetail = useOwnedRead(object, context, () => setDetail("Loading object"));
    const readList = useOwnedRead("project", context, () => setIndependent("Loading list"));
    useEffect(() => {
      void readDetail(asked => Promise.all([
        openItemDraft({ context: asked, ref: { kind: "environment", id: object } }),
        listWholeCatalog({ context: asked, kind: "observation", filter: {} }),
      ]), ([answer]) => setDetail(answer.draft?.name ?? answer.reason ?? "No object"));
    }, [object, readDetail]);
    useEffect(() => {
      void readList(asked => listWholeCatalog({ context: asked, kind: "case", filter: {} }), answer => setIndependent(answer.page?.snapshot ?? answer.reason ?? "No list"));
    }, [readList]);
    return <><button onClick={() => setObject("B")}>Object B</button><p>{detail}</p><p>{independent}</p></>;
  }
  render(<Harness />);
  await waitFor(() => expect(details).toHaveLength(1));
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "Object B" }));
  await waitFor(() => expect(details).toHaveLength(2));
  const reply = (row: typeof lists[number], snapshot: string): CatalogResult => ({ state: "completed", context: row.request.context, page: { items: [], total: 0, snapshot, incomplete: [], recorded: true } });
  const independent = lists.find(row => row.request.kind === "case")!;
  independent.resolve(reply(independent, "Independent listing committed"));
  const observations = lists.filter(row => row.request.kind === "observation");
  details[1]!.resolve({ state: "completed", context: details[1]!.request.context, new: false, draft: { name: "Object B details" } });
  observations[1]!.resolve(reply(observations[1]!, "B"));
  expect(await screen.findByText("Object B details")).toBeTruthy();
  await act(async () => {
    details[0]!.resolve({ state: "failed", context: details[0]!.request.context, new: false, reason: "Old object refused" });
    observations[0]!.resolve(reply(observations[0]!, "A"));
  });
  await waitFor(() => expect(screen.queryByText("Old object refused")).toBeNull());
  expect(screen.getByText("Object B details")).toBeTruthy();
  expect(screen.getByText("Independent listing committed")).toBeTruthy();
});
