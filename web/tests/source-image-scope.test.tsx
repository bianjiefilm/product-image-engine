import {it,expect,vi,beforeEach} from "vitest";
import type {ReactElement} from "react";
const h=vi.hoisted(()=>({at:0,slots:[] as unknown[],effects:[] as Array<()=>void>,cleanups:[] as Array<()=>void>,events:new Map<string,()=>void>()}));
vi.mock("react",async importOriginal=>({...await importOriginal<typeof import("react")>(),
 useState:(initial:unknown)=>{const n=h.at++;if(!(n in h.slots))h.slots[n]=typeof initial==="function"?(initial as ()=>unknown)():initial;return[h.slots[n],(v:unknown)=>{h.slots[n]=typeof v==="function"?(v as (old:unknown)=>unknown)(h.slots[n]):v}]},
 useRef:(initial:unknown)=>{const n=h.at++;if(!(n in h.slots))h.slots[n]={current:initial};return h.slots[n]},
 useEffect:(effect:()=>unknown,deps:unknown[])=>{const n=h.at++,old=h.slots[n] as {deps:unknown[],cleanup?:()=>void}|undefined;if(!old||deps.some((v,i)=>v!==old.deps[i])){h.effects.push(()=>{old?.cleanup?.();const cleanup=effect();h.slots[n]={deps,cleanup:typeof cleanup==="function"?cleanup:undefined};if(typeof cleanup==="function")h.cleanups.push(cleanup as ()=>void)})}},
}));
import {SourceImagePanel}from "@/components/source-image/Panel";
import {quoted,capabilities}from "./source-image-fixture";
type Props={view:ReturnType<typeof quoted>|null;history:ReturnType<typeof quoted>[];capabilities:ReturnType<typeof capabilities>|null;projectID:string;onHistory:(run:string)=>void;onQuote:()=>void;onConfirm:()=>void};
function render(projectId?:string){h.at=0;return SourceImagePanel({projectId}) as ReactElement<Props>}
function effects(){const a=h.effects.splice(0);for(const f of a)f()}
const tick=()=>new Promise<void>(r=>setImmediate(r));
beforeEach(()=>{h.at=0;h.slots=[];h.effects=[];h.cleanups=[];h.events.clear();vi.restoreAllMocks();vi.unstubAllGlobals();vi.stubGlobal("window",{location:{search:"",href:"http://localhost/projects/proj_a"},history:{replaceState:()=>{}},addEventListener:(n:string,f:()=>void)=>h.events.set(n,f),removeEventListener:(n:string)=>h.events.delete(n)});vi.stubGlobal("document",{visibilityState:"visible",addEventListener:(n:string,f:()=>void)=>h.events.set(n,f),removeEventListener:(n:string)=>h.events.delete(n)});vi.stubGlobal("sessionStorage",{getItem:()=>null,setItem:()=>{},removeItem:()=>{}})});
it("independent late authorize completion must not repopulate previous project into current mount",async()=>{
 let sessions=0,release!:(r:Response)=>void;const calls:string[]=[];
 vi.stubGlobal("fetch",async(u:string,i?:RequestInit)=>{calls.push(u);if(u==="/api/auth/session"){sessions++;if(sessions===2)return new Promise<Response>(r=>release=r);return Response.json({principal:{user_id:"usr_a",account_id:"acct_a"}})}if(u.includes("capabilities"))return Response.json(capabilities());if(u.endsWith("/sir_a"))return Response.json(quoted());return Response.json({runs:[],next_cursor:""})});
 const initial=render("proj_a");effects();await tick();initial.props.onHistory("sir_a");await tick();expect(sessions).toBe(2);
 render("proj_b");effects();await tick();
 release(Response.json({principal:{user_id:"usr_a",account_id:"acct_a"}}));await tick();await tick();
 const current=render("proj_b");expect(current.props.projectID).toBe("proj_b");expect(current.props.view,"late old-project authorized operation displayed in new project: "+JSON.stringify(calls)).toBeNull();
 for(const f of h.cleanups)f();
});

it.each(["project","pagehide","unmount"])("stale project-create principal cannot dispatch after %s",async boundary=>{
 let release!:(r:Response)=>void;const calls:string[]=[];
 vi.stubGlobal("fetch",async(u:string)=>{calls.push(u);if(u==="/api/auth/session"){if(!release)return new Promise<Response>(r=>release=r);return Response.json({principal:{user_id:"usr_a",account_id:"acct_a"}})}throw new Error("stale dispatched "+u)});
 const initial=render();effects();initial.props.onQuote();await tick();
 if(boundary==="project"){render("proj_b");effects()}else if(boundary==="pagehide"){h.events.get("pagehide")!()}else for(const f of h.cleanups)f();
 release(Response.json({principal:{user_id:"usr_a",account_id:"acct_a"}}));await tick();await tick();
 expect(calls.filter(u=>u==="/api/projects")).toEqual([]);
});
it.each(["caps","history","detail"])("project transition fences pending old %s completion",async boundary=>{
 let hold=false,release!:(r:Response)=>void;const calls:string[]=[];
 vi.stubGlobal("fetch",async(u:string)=>{calls.push(u);if(u==="/api/auth/session")return Response.json({principal:{user_id:"usr_a",account_id:"acct_a"}});
 if(hold&&u.includes("proj_a")&&(boundary==="caps"?u.includes("capabilities"):boundary==="detail"?u.endsWith("/sir_a"):u.endsWith("source-image-runs")))return new Promise<Response>(r=>release=r);
 if(u.includes("capabilities"))return Response.json(capabilities());if(u.endsWith("/sir_a"))return Response.json(quoted());return Response.json({runs:[],next_cursor:""})});
 render("proj_a");effects();await tick();hold=true;render("proj_a").props.onHistory("sir_a");await tick();
 render("proj_b");effects();await tick();const before=calls.length;
 release(boundary==="caps"?Response.json(capabilities()):boundary==="detail"?Response.json(quoted()):Response.json({runs:[quoted()],next_cursor:""}));await tick();await tick();
 const current=render("proj_b");expect(current.props.view).toBeNull();expect(calls.slice(before).filter(u=>u.includes("proj_a"))).toEqual([]);
 for(const f of h.cleanups)f();
});

it("old handler cannot dispatch during project render before effect cleanup",async()=>{
 const calls:string[]=[];vi.stubGlobal("fetch",async(u:string)=>{calls.push(u);if(u==="/api/auth/session")return Response.json({principal:{user_id:"usr_a",account_id:"acct_a"}});if(u.includes("capabilities"))return Response.json(capabilities());return Response.json({runs:[],next_cursor:""})});
 const old=render("proj_a");effects();await tick();render("proj_b");const before=calls.length;old.props.onHistory("sir_a");await tick();expect(calls.slice(before)).toEqual([]);expect(render("proj_b").props.view).toBeNull();for(const f of h.cleanups)f();
});
it.each(["response","body","account","authorize","caps","history"])("stale new-project %s continuation cannot bind old creation",async stage=>{
 let release!:(r:Response)=>void,releaseBody!:(r:{project:{id:string}})=>void,sessions=0;const calls:string[]=[];
 vi.stubGlobal("fetch",async(u:string)=>{calls.push(u);
 if(u==="/api/auth/session"){sessions++;if(stage==="account"&&sessions===2||stage==="authorize"&&sessions===3)return new Promise<Response>(r=>release=r);return Response.json({principal:{user_id:"usr_a",account_id:"acct_a"}})}
 if(u==="/api/projects"){if(stage==="response")return new Promise<Response>(r=>release=r);const r=Response.json({project:{id:"proj_a"}});if(stage==="body")vi.spyOn(r,"json").mockImplementation(()=>new Promise<{project:{id:string}}>(resolve=>releaseBody=resolve));return r;}
 if(u.includes("proj_a")&&(stage==="caps"&&u.includes("capabilities")||stage==="history"&&u.endsWith("source-image-runs")))return new Promise<Response>(r=>release=r);
 if(u.includes("capabilities"))return Response.json(capabilities());return Response.json({runs:[],next_cursor:""});});
 const initial=render();effects();initial.props.onQuote();await tick();expect(stage==="body"?releaseBody:release).toBeDefined();render("proj_b");effects();await tick();const before=calls.length;
 if(stage==="body")releaseBody({project:{id:"proj_a"}});else release(stage==="caps"?Response.json(capabilities()):stage==="history"?Response.json({runs:[quoted()],next_cursor:""}):stage==="response"?Response.json({project:{id:"proj_a"}}):Response.json({principal:{user_id:"usr_a",account_id:"acct_a"}}));await tick();await tick();
 const current=render("proj_b");expect(current.props.projectID).toBe("proj_b");expect(current.props.view).toBeNull();expect(current.props.history).toEqual([]);expect(calls.slice(before)).toEqual([]);for(const f of h.cleanups)f();
});
it("same-project history survives legitimate reads and changed-account confirm cannot pay old quote",async()=>{
 let account="acct_a";const calls:Array<[string,string]>=[];
 vi.stubGlobal("fetch",async(u:string,i?:RequestInit)=>{calls.push([u,i?.method??"GET"]);if(u==="/api/auth/session")return Response.json({principal:{user_id:"usr_a",account_id:account}});if(u.includes("capabilities"))return Response.json(capabilities());if(u.endsWith("/sir_a"))return Response.json(quoted());return Response.json({runs:account==="acct_a"?[quoted()]:[],next_cursor:""})});
 render("proj_a");effects();await tick();render("proj_a").props.onHistory("sir_a");await tick();expect(render("proj_a").props.view?.run_id).toBe("sir_a");
 account="acct_b";render("proj_a").props.onConfirm();await tick();const current=render("proj_a");expect(current.props.view).toBeNull();expect(current.props.history).toEqual([]);expect(calls.filter(([,method])=>method!=="GET")).toEqual([]);for(const f of h.cleanups)f();
});

it.each(["pagehide","unmount"])("late action principal cannot restore scope after %s",async boundary=>{
 let sessions=0,release!:(r:Response)=>void;const calls:string[]=[];
 vi.stubGlobal("fetch",async(u:string)=>{calls.push(u);if(u==="/api/auth/session"){sessions++;if(sessions===2)return new Promise<Response>(r=>release=r);return Response.json({principal:{user_id:"usr_a",account_id:"acct_a"}})}if(u.includes("capabilities"))return Response.json(capabilities());if(u.endsWith("/sir_a"))return Response.json(quoted());return Response.json({runs:[],next_cursor:""})});
 render("proj_a");effects();await tick();render("proj_a").props.onHistory("sir_a");await tick();
 if(boundary==="pagehide")h.events.get("pagehide")!();else for(const f of h.cleanups)f();const before=calls.length;
 release(Response.json({principal:{user_id:"usr_a",account_id:"acct_a"}}));await tick();await tick();expect(calls.slice(before)).toEqual([]);expect(render("proj_a").props.view).toBeNull();for(const f of h.cleanups)f();
});
