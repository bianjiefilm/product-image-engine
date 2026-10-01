import { SERVER_BASE, ACCESS_COOKIE, serverHeaders } from "@/lib/server";
import { decodeSourceRun, decodeSourceCapabilities, decodeSourceList, parseSourceJSON, sourceSegment, isMinor } from "@/lib/source-image";

const headers={ "Cache-Control":"private, no-store", "X-Content-Type-Options":"nosniff" };
const messages: Record<string,string>={ unauthenticated:"请重新登录", forbidden:"当前工程不允许此操作", not_found:"资源不存在", invalid_request:"请求不合法", conflict:"原请求事实冲突，请重新读取", source_unconfigured:"正式生成尚未配置", source_unavailable:"正式服务暂不可用，原请求状态待核实", method_not_allowed:"此资源不接受该方法" };
class Rejected extends Error { constructor(public status: number, public code: string) { super(code); } }
const reject=(status: number, code: string): never => { throw new Rejected(status,code); };
function errorResponse(status: number, code: string, run?: string): Response { return Response.json({error:{code,message:messages[code]},...(run ? {run_id:run} : {})},{status,headers}); }
function token(req: Request): string {
  const matches=(req.headers.get("cookie") ?? "").split(";").map(s=>s.trim()).filter(s=>s.split("=",1)[0] === ACCESS_COOKIE);
  if(matches.length !== 1) return reject(401,"unauthenticated");
  let value: string; try { value=decodeURIComponent(matches[0].slice(ACCESS_COOKIE.length+1)); } catch {return reject(401,"unauthenticated");}
  if(!value || value.length>8192 || !/^[A-Za-z0-9._~-]+$/.test(value)) return reject(401,"unauthenticated");
  return value;
}
function mutationOrigin(req: Request, url: URL): void {
  const origin=req.headers.get("origin"); const site=req.headers.get("sec-fetch-site");
  if(origin !== url.origin || site !== null && site !== "same-origin" && site !== "none") reject(403,"forbidden");
}
async function boundedBytes(body: ReadableStream<Uint8Array> | null, max: number, signal: AbortSignal): Promise<Uint8Array> {
  if(!body) return new Uint8Array(); const reader=body.getReader(); const chunks: Uint8Array[]=[]; let n=0;
  const cancel=()=>{void reader.cancel().catch(()=>{});}; signal.addEventListener("abort",cancel,{once:true});
  try { while(true) { if(signal.aborted) throw new Error("aborted"); const next=await reader.read(); if(signal.aborted) throw new Error("aborted"); if(next.done) break; n+=next.value.byteLength; if(n>max) throw new Error("oversize"); chunks.push(next.value); }
    const bytes=new Uint8Array(n); let at=0; for(const chunk of chunks) {bytes.set(chunk,at); at+=chunk.length;} return bytes;
  } catch(e) { await reader.cancel().catch(()=>{}); throw e; } finally {signal.removeEventListener("abort",cancel); reader.releaseLock();}
}
function record(v: unknown): Record<string,unknown> { if(!v || typeof v !== "object" || Array.isArray(v)) return reject(400,"invalid_request"); return v as Record<string,unknown>; }
function fields(v: Record<string,unknown>, allowed: string[]): void { if(Object.keys(v).length !== allowed.length || allowed.some(k=>!Object.hasOwn(v,k))) reject(400,"invalid_request"); }
function cleanKey(v: unknown): v is string { return typeof v === "string" && !!v && v.trim() === v && new TextEncoder().encode(v).length<=256 && !/[\u0000-\u001f\u007f]/.test(v); }
function cursor(v: string): void {
  if(!/^[A-Za-z0-9_-]{1,1024}$/.test(v)) reject(400,"invalid_request");
  try { const bytes=Buffer.from(v,"base64url"); if(bytes.toString("base64url") !== v) reject(400,"invalid_request"); const c=record(parseSourceJSON(new TextDecoder("utf-8",{fatal:true}).decode(bytes))); fields(c,["v","created_at","id"]); if(c.v !== 1 || !isMinor(c.created_at) || c.created_at === "0" || !sourceSegment(c.id) || !c.id.startsWith("sir_")) reject(400,"invalid_request"); } catch {reject(400,"invalid_request");}
}
function queryFor(url: URL, resource: string, method: string): string {
  if(/%(?![0-9a-fA-F]{2})/.test(url.search)) reject(400,"invalid_request");
  const allowed=method === "GET" && resource === "list" ? ["limit","cursor"] : resource === "find" ? ["request_key"] : [];
  const seen=new Set<string>(); for(const [key,value] of url.searchParams) { if(!allowed.includes(key) || seen.has(key)) reject(400,"invalid_request"); seen.add(key);
    if(key === "limit" && (!/^[1-9][0-9]{0,2}$/.test(value) || Number(value)>100)) reject(400,"invalid_request");
    if(key === "cursor") cursor(value);
    if(key === "request_key" && !cleanKey(value)) reject(400,"invalid_request");
  }
  if(resource === "find" && !seen.has("request_key")) reject(400,"invalid_request");
  return url.searchParams.size ? "?"+url.searchParams.toString() : "";
}
async function requestBody(req: Request, resource: string, action: string | undefined, signal: AbortSignal): Promise<string | undefined> {
  let bytes: Uint8Array; try { bytes=await boundedBytes(req.body,32768,signal); } catch {return reject(400,"invalid_request");}
  if(req.method === "GET") { if(bytes.length) reject(400,"invalid_request"); return undefined; }
  if(bytes.length && req.headers.get("content-type")?.split(";",1)[0].trim().toLowerCase() !== "application/json") reject(400,"invalid_request");
  let raw: string; let b: Record<string,unknown>; try { raw=new TextDecoder("utf-8",{fatal:true}).decode(bytes); b=record(parseSourceJSON(raw || "{}")); } catch {return reject(400,"invalid_request");}
  const allowed=resource === "list" ? ["request_key","mode","prompt","size"] : action === "confirm" ? ["quote_id","quote_fingerprint"] : [];
  fields(b,allowed); if(allowed.some(k=>typeof b[k] !== "string")) reject(400,"invalid_request");
  if(resource === "list" && (!cleanKey(b.request_key) || b.mode !== "text_generate" || b.size !== "1024*1024" || typeof b.prompt !== "string" || !b.prompt.trim() || new TextEncoder().encode(b.prompt).length>16384)) reject(400,"invalid_request");
  if(action === "confirm" && (!sourceSegment(b.quote_id) || typeof b.quote_fingerprint !== "string" || !/^[0-9a-f]{64}$/.test(b.quote_fingerprint))) reject(400,"invalid_request");
  return raw || undefined; // No parse/re-serialize or fabricated {} body.
}
export async function sourceProxy(req: Request, p: { id: string; runId?: string; action?: string }, resource="list"): Promise<Response> {
  const abort=new AbortController(); const timeout=setTimeout(()=>abort.abort(),20000);
  const onCancel=()=>abort.abort(); req.signal.addEventListener("abort",onCancel,{once:true}); if(req.signal.aborted) abort.abort();
  let streaming=false;
  const cleanup=()=>{clearTimeout(timeout); req.signal.removeEventListener("abort",onCancel);};
  try {
    const url=new URL(req.url);
    if(!sourceSegment(p.id) || p.runId !== undefined && (!sourceSegment(p.runId) || !p.runId.startsWith("sir_"))) reject(400,"invalid_request");
    const actionMethods: Record<string,string>={confirm:"POST",reconcile:"POST",cancel:"POST",select:"POST",output:"DELETE",content:"GET",export:"GET"};
    const allowed=resource === "list" ? ["GET","POST"] : resource === "action" ? [actionMethods[p.action ?? ""]].filter(Boolean) : ["GET"];
    if(resource === "action" && !Object.hasOwn(actionMethods,p.action ?? "")) reject(404,"not_found");
    if(!allowed.includes(req.method)) reject(405,"method_not_allowed");
    const suffix=resource === "capabilities" ? "source-image-capabilities" : "source-image-runs"+(resource === "find" ? "/by-request" : (resource === "get" || resource === "action") && p.runId ? "/"+encodeURIComponent(p.runId)+(resource === "action" ? "/"+p.action : "") : "");
    const path="/projects/"+encodeURIComponent(p.id)+"/"+suffix;
    if(url.pathname !== "/api"+path || url.hash) reject(400,"invalid_request");
    const access=token(req); if(req.method !== "GET") mutationOrigin(req,url);
    const query=queryFor(url,resource,req.method); const body=await requestBody(req,resource,p.action,abort.signal);
    const upstream=await fetch(SERVER_BASE+"/api/v1"+path+query,{method:req.method,headers:{...serverHeaders(access),...(body !== undefined ? {"Content-Type":"application/json"} : {})},body,cache:"no-store",redirect:"manual",signal:abort.signal});
    if(upstream.status>=300 && upstream.status<400) { await upstream.body?.cancel(); reject(503,"source_unavailable"); }
    if(!upstream.ok) {
      let d: Record<string,unknown>={}; try {d=record(parseSourceJSON(new TextDecoder("utf-8",{fatal:true}).decode(await boundedBytes(upstream.body,1048576,abort.signal))));} catch {}
      const status=[400,401,403,404,409].includes(upstream.status) ? upstream.status : 503;
      const rawCode=d.error && typeof d.error === "object" ? (d.error as Record<string,unknown>).code : "";
      const fallback: Record<number,string>={400:"invalid_request",401:"unauthenticated",403:"forbidden",404:"not_found",409:"conflict",503:"source_unavailable"};
      const code=typeof rawCode === "string" && ["source_unavailable","source_unconfigured"].includes(rawCode) && status === 503 ? rawCode : fallback[status];
      const run=status !== 404 && sourceSegment(d.run_id) && d.run_id.startsWith("sir_") ? d.run_id : undefined;
      return errorResponse(status,code,run);
    }
    if(resource === "action" && (p.action === "content" || p.action === "export")) {
      const image=p.action === "content", max=image ? 16777216 : 17825792, mime=image ? "image/png" : "application/zip";
      const length=upstream.headers.get("content-length");
      if(upstream.status !== 200 || upstream.headers.get("content-type")?.split(";",1)[0] !== mime || !upstream.body || length !== null && (!/^(0|[1-9][0-9]*)$/.test(length) || BigInt(length)>BigInt(max))) { await upstream.body?.cancel(); reject(503,"source_unavailable"); }
      const reader=upstream.body!.getReader(); let size=0; let closed=false;
      const end=async()=>{if(closed) return; closed=true; await reader.cancel().catch(()=>{}); reader.releaseLock(); cleanup();};
      const stream=new ReadableStream<Uint8Array>({async pull(controller) {try { if(abort.signal.aborted) throw new Error("aborted"); const chunk=await reader.read(); if(abort.signal.aborted) throw new Error("aborted"); if(chunk.done) { if(length !== null && size !== Number(length)) throw new Error("length_mismatch"); controller.close(); await end(); return; } size+=chunk.value.length; if(size>max) throw new Error("oversize"); controller.enqueue(chunk.value); } catch {controller.error(new Error("source_content_unavailable")); await end();}}, async cancel() {abort.abort(); await end();} });
      abort.signal.addEventListener("abort",()=>{void end();},{once:true}); streaming=true;
      return new Response(stream,{status:200,headers:{...headers,"Content-Type":mime,"Content-Disposition":image ? 'inline; filename="source-image.png"' : 'attachment; filename="source-image.zip"'}});
    }
    const data=parseSourceJSON(new TextDecoder("utf-8",{fatal:true}).decode(await boundedBytes(upstream.body,1048576,abort.signal)));
    const safe=resource === "capabilities" ? decodeSourceCapabilities(data) : resource === "list" && req.method === "GET" ? decodeSourceList(data) : decodeSourceRun(data);
    if("project_id" in safe && safe.project_id !== p.id || "runs" in safe && safe.runs.some(r=>r.project_id !== p.id)) reject(503,"source_unavailable");
    return Response.json(safe,{status:200,headers});
  } catch(e) { return e instanceof Rejected ? errorResponse(e.status,e.code) : errorResponse(503,"source_unavailable"); } finally {if(!streaming) cleanup();}
}
