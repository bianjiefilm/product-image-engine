import {it,expect,vi,beforeEach} from "vitest";
import {sourceProxy} from "@/app/api/projects/[id]/source-image-runs/proxy";
import {quoted} from "./source-image-fixture";
const url="http://localhost/api/projects/proj_a/source-image-runs/by-request?";
beforeEach(()=>{vi.restoreAllMocks();vi.unstubAllGlobals()});
it.each(["%ff","%C0%AF","%ED%A0%80","%E2%82","%80","%F4%90%80%80","%E2%28%A1","%C2","%F0%80%80%AF","%"])("invalid raw UTF8 %s rejects keys and values with zero upstream",async encoded=>{
 const fetch=vi.fn(async()=>Response.json(quoted()));vi.stubGlobal("fetch",fetch);
 for(const query of ["request_key="+encoded,"request_key"+encoded+"=good"]){const r=await sourceProxy(new Request(url+query,{headers:{cookie:"pia_access=token"}}),{id:"proj_a"},"find");expect(r.status).toBe(400)}expect(fetch).not.toHaveBeenCalled();
});
it.each(["%EF%BF%BD","%E4%B8%AD%E6%96%87","a%2Bb","a+b","%F0%9F%8E%A8"])("valid UTF8/plus %s preserves legitimate request key",async encoded=>{
 const calls:string[]=[];vi.stubGlobal("fetch",async(u:string)=>{calls.push(u);return Response.json(quoted())});
 const r=await sourceProxy(new Request(url+"request_key="+encoded,{headers:{cookie:"pia_access=token"}}),{id:"proj_a"},"find");expect(r.status).toBe(200);expect(calls).toHaveLength(1);expect(new URL(calls[0]).searchParams.get("request_key")).toBe(new URL(url+"request_key="+encoded).searchParams.get("request_key"));
});
