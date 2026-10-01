"use client";
import { useEffect, useRef, useState } from "react";
import { Card, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { SourceImageController, sourceImageAPI, canConfirm, formatMinor, requestState, sourcePath, sourceSegment, pendingHintKey, parseSourceJSON, SourceHTTPError, sourceWritesDenied } from "@/lib/source-image";
import type { SourceRunView, SourceCapabilities, SourceScope } from "@/lib/source-image";

type ScreenProps={ view: SourceRunView | null; capabilities: SourceCapabilities | null; nowUnix: number; projectID: string; busy?: boolean; prompt?: string; notice?: string; deleteArmed?: boolean; history?: SourceRunView[]; nextCursor?: string; pendingKey?: string;
  onPrompt?: (v: string)=>void; onQuote?: ()=>void; onConfirm?: ()=>void; onRefresh?: ()=>void; onReload?: ()=>void; onNew?: ()=>void; onAction?: (name:"reconcile"|"cancel"|"select"|"deleteOutput")=>void; onArmDelete?: ()=>void; onHistory?: (run:string)=>void; onMore?: ()=>void;
};
function expiry(at: number): string { const d=new Date(at*1000); return Number.isNaN(d.getTime()) ? "到期时间待核实" : d.toISOString().replace("T"," ").replace(".000Z"," UTC"); }
export function SourceImageScreen(p: ScreenProps) {
  const v=p.view, mode=p.capabilities?.modes.find(m=>m.mode === "text_generate"), q=v?.quote;
  const pay=!!v && mode?.can_confirm === true && canConfirm(v,p.nowUnix);
  const output=!!v?.output && !v.deleted && v.payment.status !== "refunded" && v.payment.status !== "released";
  const writeDenied=sourceWritesDenied(p.capabilities);
  return <Card data-source-entry="true" data-runtime-ready={mode?.ready === true ? "true":"false"}>
    <CardHeader><CardTitle>普通文字生成</CardTitle><CardDescription>普通概念图，不宣称商品主体保真。原输出为 1024×1024，1 张；不会改写工程原有尺寸。</CardDescription></CardHeader>
    {mode?.ready !== true ? <p role="status">{mode?.can_quote ? "报价与明确确认可用；自动结果恢复尚未配置，完整生成闭环尚不可用。" : "正式生成尚未配置或当前工程无写权限；已有记录仍需服务端授权读取。"}</p> : null}
    {p.projectID ? <><label>文字描述<textarea rows={3} value={p.prompt ?? ""} onChange={e=>p.onPrompt?.(e.target.value)} placeholder="例如：蓝色纸盒，浅灰棚拍" /></label><div className="row"><Button type="button" disabled={!!p.busy || mode?.can_quote !== true || !(p.prompt ?? "").trim()} onClick={p.onQuote}>获取报价</Button><Button type="button" variant="outline" disabled={!!p.busy} onClick={p.onNew}>明确重新生成（新请求）</Button></div></> : <Button type="button" disabled={!!p.busy} onClick={p.onQuote}>创建个人普通图工程（1024×1024）</Button>}
    {v ? <><p>{requestState(v)}</p><p>原请求 {v.run_id}{v.task_id ? ` · Task ${v.task_id}` : ""}{v.task_phase ? ` · ${v.task_phase}` : ""}</p>
      <p>付款人：{v.payment.payer_label} · {v.payment.payer_account_id}</p>
      {q ? <p>原报价 {formatMinor(q.amount_minor)} · {v.model} · {v.quantity} 张 · 1024×1024 · 到期 {expiry(q.expires_at_unix)}</p> : <p>报价未验证，金额待核实</p>}
      {pay ? <Button type="button" disabled={!!p.busy} onClick={p.onConfirm}>确认支付并生成 {formatMinor(q?.amount_minor)}</Button> : null}
      <p>原扣款 {formatMinor(v.payment.original_charged_minor ?? v.payment.charged_minor)}{v.payment.original_charge_id || v.payment.charge_id ? ` · ${v.payment.original_charge_id || v.payment.charge_id}` : ""}</p>
      {v.payment.status === "refunded" ? <p>原退款记录 {v.payment.refund_id || "编号待核实"}；{v.payment.refund_amount_minor === null ? "退款金额待核实" : formatMinor(v.payment.refund_amount_minor)}</p> : null}
      <p>当前费用{v.payment.refresh_status === "unknown" ? "待核实":"未刷新"}；核验时间{v.payment.last_verified_at === null ? "未知":expiry(v.payment.last_verified_at)}。图片出现不代表最新费用通过。</p>
      <div className="row"><Button type="button" variant="outline" disabled={!!p.busy} onClick={p.onRefresh}>读取同一记录</Button><Button type="button" variant="outline" disabled={!!p.busy || writeDenied || v.deleted} onClick={()=>p.onAction?.("reconcile")}>恢复原任务</Button><Button type="button" variant="outline" disabled={!!p.busy || writeDenied || v.cancel_requested || v.deleted} onClick={()=>p.onAction?.("cancel")}>请求取消原任务</Button></div>
      {output ? <><p>原输出当前可用性待核实；打开或导出时会再次检查原账单与素材权限。</p><figure><img alt="普通概念图，视觉质量待核实" src={sourcePath(p.projectID,v.run_id,"content")} style={{maxWidth:"100%"}} /><figcaption>原输出 1024×1024；视觉质量待核实</figcaption></figure><div className="row"><Button type="button" disabled={!!p.busy || writeDenied || v.selected || v.payment.status !== "charged"} onClick={()=>p.onAction?.("select")}>{v.selected ? "已选择原输出":"选择原输出"}</Button><a href={sourcePath(p.projectID,v.run_id,"export")}>导出原图与事实报告</a><Button type="button" variant="outline" disabled={!!p.busy || writeDenied} onClick={p.onArmDelete}>删除输出展示</Button></div></> : null}
      {v.output && !v.deleted && !output ? <Button type="button" variant="outline" disabled={!!p.busy || writeDenied} onClick={p.onArmDelete}>删除输出展示</Button> : null}
      {v.output && !v.deleted && p.deleteArmed ? <p>确认隐藏原输出并申请释放原引用。不会退款，也不代表物理文件已删除。<Button type="button" disabled={!!p.busy || writeDenied} onClick={()=>p.onAction?.("deleteOutput")}>确认删除输出展示</Button></p> : null}
      {v.deleted ? <p>输出已隐藏，引用清理待核实；没有退款或物理删除完成承诺。</p> : null}
      <p>视觉质量待核实 · 主体保真不适用 · 人类采用 NOT_RUN</p>
    </> : <p>先取得服务端正式报价，再单独明确确认。未知费用不会按零处理。</p>}
    {p.notice ? <p role="alert">{p.notice}</p> : null}
    {p.pendingKey ? <p>待核实原请求键：{p.pendingKey}（本地仅作查找提示，事实以服务端为准）</p> : null}
    {p.projectID ? <Button type="button" variant="outline" disabled={!!p.busy} onClick={p.onReload}>重新读取当前登录与历史</Button> : null}
    <h3>原请求历史</h3>{(p.history ?? []).map(r=><p key={r.run_id}><Button type="button" variant="outline" disabled={!!p.busy} onClick={()=>p.onHistory?.(r.run_id)}>{r.run_id} · {requestState(r)} · {formatMinor(r.quote?.amount_minor)}</Button></p>)}
    {p.nextCursor ? <Button type="button" variant="outline" disabled={!!p.busy} onClick={p.onMore}>读取更多历史</Button> : null}
  </Card>;
}
async function principal(ensure:()=>void): Promise<{userID:string;accountID:string}> {
  const abort=new AbortController(); const timeout=setTimeout(()=>abort.abort(),10000);
  try { ensure(); const r=await fetch("/api/auth/session",{cache:"no-store",credentials:"same-origin",redirect:"error",signal:abort.signal}); ensure(); if(!r.ok) throw new SourceHTTPError(r.status,"unauthenticated");
    const body=await r.json() as {principal?:{user_id?:string;account_id?:string}}; ensure(); const p=body.principal;
    if(!sourceSegment(p?.user_id) || !sourceSegment(p?.account_id)) throw new Error("登录身份未验证"); return {userID:p.user_id,accountID:p.account_id};
  } finally {clearTimeout(timeout);}
}
// A lease binds async validation to the panel that dispatched it, before controller scope exists.
type PanelLease={generation:number;project:string};
export function SourceImagePanel({projectId}:{projectId?:string}) {
  const controller=useRef<SourceImageController | null>(null); if(!controller.current) controller.current=new SourceImageController(sourceImageAPI);
  const c=controller.current;
  const [,setBound]=useState(projectId ?? ""), [prompt,setPrompt]=useState(""), [busy,setBusy]=useState(false), [notice,setNotice]=useState(""), [armed,setArmed]=useState(false), [tick,setTick]=useState(0), [history,setHistory]=useState<SourceRunView[]>([]), [nextCursor,setNextCursor]=useState("");
  const verifiedScope=useRef<SourceScope | null>(null), mounted=useRef(true);
  const binding=useRef({prop:projectId,project:projectId ?? "",generation:0});
  const operationRunning=useRef<PanelLease | null>(null);
  // Invalidate synchronously on render: the old effect may not have cleaned up yet.
  if(binding.current.prop !== projectId) {
    binding.current={prop:projectId,project:projectId ?? "",generation:binding.current.generation+1};
    c.setScope(null);verifiedScope.current=null;operationRunning.current=null;
  }
  const rendered={generation:binding.current.generation,project:binding.current.project};
  const current=(l:PanelLease)=>mounted.current && l.generation === binding.current.generation && l.project === binding.current.project;
  function ensure(l:PanelLease): void {if(!current(l)) throw new Error("页面或登录范围已改变");}
  const repaint=()=>{if(mounted.current) setTick(n=>n+1);}; void tick;
  function invalidate(): void {binding.current.generation++;c.setScope(null);verifiedScope.current=null;operationRunning.current=null;}
  async function step<T>(l:PanelLease,work:()=>Promise<T>): Promise<T> {ensure(l);const value=await work();ensure(l);return value;}
  async function authorize(l:PanelLease): Promise<SourceScope> {
    const p=await step(l,()=>principal(()=>ensure(l))), s={...p,projectID:l.project};ensure(l);
    const previous=verifiedScope.current, changed=JSON.stringify(s) !== JSON.stringify(previous);
    if(previous && changed) {const ownsOperation=operationRunning.current === l;invalidate();l.generation=binding.current.generation;if(ownsOperation)operationRunning.current=l;}
    ensure(l);c.setScope(s);verifiedScope.current=s;
    if(changed) {setHistory([]);setNextCursor("");setArmed(false);repaint();}return s;
  }
  function hint(l:PanelLease): void {ensure(l);const s=verifiedScope.current;if(!s)return;try {const h=c.pendingHint;if(h)sessionStorage.setItem(pendingHintKey(s),JSON.stringify(h));else sessionStorage.removeItem(pendingHintKey(s));}catch {}}
  async function readHistory(l:PanelLease,cursor?:string): Promise<void> {
    const list=await step(l,()=>c.history(cursor));ensure(l);
    setHistory(prev=>cursor ? [...prev,...list.runs.filter(r=>!prev.some(p=>p.run_id === r.run_id))] : list.runs);setNextCursor(list.next_cursor);
  }
  useEffect(()=>{
    mounted.current=true;const l={generation:binding.current.generation,project:binding.current.project};
    setBound(l.project);c.setScope(null);verifiedScope.current=null;setHistory([]);setNextCursor("");setArmed(false);setBusy(false);repaint();
    if(l.project && sourceSegment(l.project)) void (async()=>{try {
      await authorize(l);await step(l,()=>c.loadCapabilities());await readHistory(l);ensure(l);
      const run=new URLSearchParams(window.location.search).get("source_run");
      if(run && sourceSegment(run)) await step(l,()=>c.restore(l.project,run));
      else {try {ensure(l);const raw=sessionStorage.getItem(pendingHintKey(verifiedScope.current!));if(raw)await step(l,()=>c.restoreHint(parseSourceJSON(raw)));}catch(e){ensure(l);void e;}}
      ensure(l);repaint();
    }catch {if(current(l)){invalidate();setHistory([]);setNextCursor("");setNotice("当前登录或工程未验证，请重新读取。");repaint();}}})();
    return()=>{if(current(l)){invalidate();mounted.current=false;}};
  // Effects perform only session/capabilities/history/original-run GET reads.
  },[projectId,c]);
  useEffect(()=>{
    const interval=setInterval(()=>{if(c.view?.quote)repaint();},1000);
    const hide=()=>{invalidate();setHistory([]);setNextCursor("");setArmed(false);setBusy(false);repaint();};
    const onVisibility=()=>{if(document.visibilityState !== "visible")hide();};
    window.addEventListener("pagehide",hide);document.addEventListener("visibilitychange",onVisibility);
    return()=>{clearInterval(interval);window.removeEventListener("pagehide",hide);document.removeEventListener("visibilitychange",onVisibility);invalidate();mounted.current=false;};
  },[c]);
  async function operate(work:(l:PanelLease)=>Promise<void>): Promise<void> {
    const l={...rendered};if(!current(l) || operationRunning.current)return;
    operationRunning.current=l;setBusy(true);setNotice("");
    try {
      if(l.project){await authorize(l);await step(l,()=>c.loadCapabilities());}
      await step(l,()=>work(l));hint(l);
      if(c.view){ensure(l);const url=new URL(window.location.href);url.searchParams.set("source_run",c.view.run_id);window.history.replaceState(null,"",url);await readHistory(l);}
    }catch(e){if(current(l)){
      hint(l);setNotice(e instanceof SourceHTTPError ? e.status === 401 ? "请重新登录；原请求需服务端再次授权" : e.status === 409 ? "原请求事实冲突，请读取原记录" : "原请求状态待核实，请读取原记录或用原请求键恢复" : e instanceof Error ? e.message : "原请求状态待核实");
      if(e instanceof SourceHTTPError && [401,403,404].includes(e.status)){invalidate();setHistory([]);setNextCursor("");setBusy(false);repaint();}
    }}finally{if(current(l)){if(operationRunning.current === l)operationRunning.current=null;setBusy(false);repaint();}}
  }
  async function quote(l:PanelLease): Promise<void> {
    if(!l.project){
      const p=await step(l,()=>principal(()=>ensure(l)));
      const r=await step(l,()=>fetch("/api/projects",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({name:"个人普通概念图",usage_kind:"文生图",width_px:1024,height_px:1024,source_type:"standalone"}),credentials:"same-origin",redirect:"error",signal:AbortSignal.timeout(10000)}));
      if(!r.ok)throw new Error("创建工程状态待核实，不代表生成或收费成功");
      const d=await step(l,()=>r.json()) as {project?:{id?:string}};if(!sourceSegment(d.project?.id))throw new Error("工程未验证");
      const after=await step(l,()=>principal(()=>ensure(l)));if(after.userID !== p.userID || after.accountID !== p.accountID)throw new Error("登录范围已改变");
      ensure(l);binding.current.generation++;binding.current.project=d.project.id;l.generation=binding.current.generation;l.project=d.project.id;
      ensure(l);setBound(l.project);await authorize(l);await step(l,()=>c.loadCapabilities());await readHistory(l);return;
    }
    await step(l,()=>c.quote({projectID:l.project,prompt,size:"1024*1024"}));
  }
  const displayed=projectId ?? binding.current.project, s=verifiedScope.current;
  const sameScope=displayed === binding.current.project && !!s && s.projectID === displayed;
  const belongs=(v:SourceRunView)=>sameScope && v.project_id === s!.projectID && v.payment.payer_user_id === s!.userID && (v.payment.payer_source !== "personal" || v.payment.payer_account_id === s!.accountID);
  const view=c.view && belongs(c.view) ? c.view:null;
  const local=(work:()=>void)=>{if(current(rendered))work();};
  return <SourceImageScreen view={view} capabilities={sameScope ? c.capabilities:null} nowUnix={Math.floor(Date.now()/1000)} projectID={displayed} prompt={prompt} busy={busy && !!operationRunning.current} notice={notice} deleteArmed={sameScope && armed} history={history.filter(belongs)} nextCursor={sameScope ? nextCursor:""} pendingKey={sameScope ? c.pendingHint?.requestKey:undefined}
    onPrompt={v=>local(()=>setPrompt(v))} onQuote={()=>void operate(quote)} onConfirm={()=>void operate(l=>step(l,()=>c.confirm()))} onRefresh={()=>void operate(l=>step(l,()=>c.restore(l.project,c.view?.run_id ?? "")))} onReload={()=>void operate(async l=>{await readHistory(l);const h=c.pendingHint;if(h)await step(l,()=>c.restoreHint(h));})}
    onNew={()=>local(()=>{try {if(operationRunning.current)throw new Error("pending");c.newRequest();hint(rendered);setArmed(false);setNotice("下一次获取报价将创建新请求，原请求保留在历史中。");repaint();}catch {setNotice("原动作尚未返回");}})} onAction={name=>void operate(async l=>{await step(l,()=>c.action(name));ensure(l);setArmed(false);})} onArmDelete={()=>local(()=>setArmed(true))} onHistory={run=>void operate(l=>step(l,()=>c.restore(l.project,run)))} onMore={()=>void operate(l=>readHistory(l,nextCursor))} />;
}
