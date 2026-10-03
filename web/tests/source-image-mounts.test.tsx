import { describe, it, expect, vi, beforeEach } from "vitest";
import type { ReactElement } from "react";
const hooks=vi.hoisted(()=>({ states:0, project:false, effects:[] as Array<()=>unknown> }));
// No browser/DOM harness is installed. Execute the actual page factories with
// deterministic hook inputs, then inspect the elements/props they compose.
vi.mock("react",async importOriginal=>({...(await importOriginal<typeof import("react")>()),
  useState:(initial:unknown)=>{const n=hooks.states++; return [hooks.project && n === 0 ? {id:"proj_a",name:"工程",status:"draft",width_px:800,height_px:800,usage_kind:"主图",source_type:"standalone"} : typeof initial === "function" ? (initial as ()=>unknown)() : initial,()=>{}];},
  useEffect:(effect:()=>unknown)=>{hooks.effects.push(effect);},useMemo:(f:()=>unknown)=>f(),useCallback:(f:unknown)=>f,useContext:()=>null,useRef:(initial:unknown)=>({current:initial}),
}));
vi.mock("next/navigation",()=>({useParams:()=>({id:"proj_a"}),useRouter:()=>({replace:()=>{}}),useSearchParams:()=>new URLSearchParams()}));
import StartPage from "@/app/start/ui";
import ProjectPage from "@/app/projects/[id]/page";
import { TextImagePanel } from "@/components/text-image/Panel";
import { SourceImagePanel } from "@/components/source-image/Panel";
function nodes(value:unknown): ReactElement<Record<string,unknown>>[] {
  if(Array.isArray(value)) return value.flatMap(nodes);
  if(!value || typeof value !== "object" || !("props" in value)) return [];
  const element=value as ReactElement<Record<string,unknown>>; return [element,...nodes(element.props.children)];
}
beforeEach(()=>{hooks.states=0;hooks.project=false;hooks.effects=[];vi.unstubAllGlobals();});
describe("normal actual page mounts and legacy read-only",()=>{
  it("both real page factories mount source creation and legacy historyOnly",()=>{
    for(const page of [StartPage,ProjectPage]) {hooks.states=0;hooks.project=page === ProjectPage; const tree=nodes(page());
      // One ordinary text panel and one limited-fidelity panel per page; never two of the same mode.
      const panels=tree.filter(n=>n.type === SourceImagePanel);
      expect(panels.filter(n=>(n.props.mode ?? "text_generate") === "text_generate")).toHaveLength(1);
      expect(panels.filter(n=>n.props.mode === "background_plate_lock")).toHaveLength(1);
      const legacy=tree.filter(n=>n.type === TextImagePanel); expect(legacy).toHaveLength(1); expect(legacy[0].props.historyOnly).toBe(true);
    }
  });
  it("historyOnly effect and explicit reload use GET with zero legacy writes",async()=>{
    const calls:Array<[string,string]>=[];vi.stubGlobal("fetch",async(u:string,i?:RequestInit)=>{calls.push([u,i?.method ?? "GET"]);return Response.json({jobs:[{id:"txt_a",job_status:"quoted"}]});});
    const tree=nodes(TextImagePanel({projectId:"proj_a",historyOnly:true}));
    for(const effect of hooks.effects) effect(); await new Promise(resolve=>setImmediate(resolve));
    expect(calls).toEqual([["/api/projects/proj_a/text-images","GET"]]);
    const buttons=tree.filter(n=>typeof n.props.onClick === "function");
    const reload=buttons.find(n=>nodes(n).length && n.props.children === "读取旧记录"); expect(reload).toBeDefined();
    await (reload!.props.onClick as ()=>unknown)(); await new Promise(resolve=>setImmediate(resolve));
    expect(calls).toEqual([["/api/projects/proj_a/text-images","GET"],["/api/projects/proj_a/text-images","GET"]]);
    expect(calls.some(([,method])=>method !== "GET")).toBe(false);
  });
  it("new personal project is an explicit separate 1024-square local action and double click coalesces",async()=>{
    const calls:Array<[string,RequestInit|undefined]>=[];
    vi.stubGlobal("fetch",async(u:string,i?:RequestInit)=>{calls.push([u,i]); if(u === "/api/auth/session") return Response.json({principal:{user_id:"usr_a",account_id:"acct_a"}});
      if(u.includes("capabilities")) return Response.json({modes:[{mode:"text_generate",can_quote:false,can_confirm:false,ready:false,disabled:true,reason:"source_unconfigured",model:"qwen-image-2.0",size:"1024*1024",fidelity:"not_applicable",visual_quality:"unknown"},{mode:"reference_edit",ready:false,disabled:true,reason:"formal_contract_unconfigured"},{mode:"background_plate_lock",ready:false,disabled:true,can_quote:false,can_confirm:false,reason:"plate_lock_disabled",size:"1024*1024",model:"qwen-image-2.0",claim:"frozen_synthetic_sample_only",fidelity:"frozen_sample_only",visual_quality:"unknown",supported_input_ids:[],supported_inputs:[]}],historical_read:true,automatic_output_recovery_ready:false});
      if(u.includes("source-image-runs")) return Response.json({runs:[],next_cursor:""});
      return Response.json({project:{id:"proj_a"}});
    });
    const element=SourceImagePanel({}) as ReactElement<{onQuote:()=>void}>;
    element.props.onQuote();element.props.onQuote();await new Promise(resolve=>setImmediate(resolve));
    const writes=calls.filter(([,i])=>i?.method === "POST");expect(writes).toHaveLength(1);expect(writes[0][0]).toBe("/api/projects");
    expect(JSON.parse(String(writes[0][1]?.body))).toEqual({name:"个人普通概念图",usage_kind:"文生图",width_px:1024,height_px:1024,source_type:"standalone"});
    expect(calls.some(([u,i])=>u.includes("source-image-runs") && i?.method === "POST")).toBe(false);
    expect(calls.some(([u])=>u.includes("text-images"))).toBe(false);
  });
});
