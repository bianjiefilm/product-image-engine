"use client";
import { useEffect, useRef } from "react";
import type { ReactNode } from "react";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Field } from "@/components/ui/field";
import { Skeleton } from "@/components/ui/skeleton";
import { Stepper } from "@/components/ui/stepper";
import styles from "@/components/ui/ui.module.css";
import { canConfirm, formatMinor, sourcePath } from "@/lib/source-image";
import type { PlateAxisState, SourceCapabilities, SourceRunView } from "@/lib/source-image";
import { NO_USABLE_PHOTO, PLATE_PROMISE, PLATE_SCOPE_NOTE, PLATE_TITLE, PRICING_NOTE, RETURN_NOTE, STEPS, axisLabel, candidateSummary, canExportPlate, canReturnPlate, canSelectPlate, charged, failedAxes, plate, plateStatusLine, reasonText, stageOf, stateLabel } from "@/lib/plate-lock";

export type PlateScreenProps = {
  view: SourceRunView | null; capabilities: SourceCapabilities | null; nowUnix: number; projectID: string;
  loading?: boolean; busy?: boolean; background?: string; inputId?: string; notice?: string;
  returnEnabled?: boolean; returnAck?: boolean; returnNotice?: string; uploadSlot?: ReactNode;
  materialsSlot?: ReactNode; railSlot?: ReactNode;
  onBackground?: (v: string) => void; onInput?: (id: string) => void; onQuote?: () => void; onConfirm?: () => void; onRefresh?: () => void;
  onSelect?: () => void; onReturn?: () => void; onReturnAck?: (v: boolean) => void; onNew?: () => void;
};

const KIND_LABEL: Record<string, string> = { carton: "纸盒", handled_metal: "金属把手杯", glass_bottle: "玻璃瓶" };
function expiry(at: number): string { const d = new Date(at * 1000); return Number.isNaN(d.getTime()) ? "到期时间待核实" : d.toISOString().replace("T", " ").replace(".000Z", " UTC"); }
const stateClass = (s: PlateAxisState): string => (s === "PASS" ? styles.stateOK : s === "FAIL" ? styles.stateBad : s === "UNKNOWN" ? styles.stateWait : styles.stateNA);

export function PlateLockScreen(p: PlateScreenProps) {
  const mode = p.capabilities?.modes.find(m => m.mode === "background_plate_lock");
  const v = p.view, pl = plate(v), q = v?.quote;
  const supported = mode?.supported_inputs ?? [];
  const ready = mode?.ready === true;
  const chosen = supported.find(i => i.input_id === p.inputId) ?? supported[0];
  const hasPhoto = supported.length > 0;
  const bg = (p.background ?? "").trim();
  const pay = !!v && mode?.can_confirm === true && canConfirm(v, p.nowUnix);
  const stage = stageOf(v, hasPhoto, bg.length > 0);
  const failed = v ? failedAxes(v) : [];
  const summary = pl ? candidateSummary(pl.candidate_state) : null;
  const contentSrc = v && pl?.result && !v.deleted ? sourcePath(p.projectID, v.run_id, "content") : "";
  // Once per quote, when the confirm button first becomes usable, move focus to it so a
  // keyboard user lands on the one decision that spends money. (autoFocus cannot do this:
  // the button mounts disabled while the quote request is still finishing.)
  const focusedQuote = useRef("");
  const quoteId = q?.quote_id ?? "";
  useEffect(() => {
    if (!pay || p.busy || !quoteId || focusedQuote.current === quoteId) return;
    const el = document.querySelector<HTMLButtonElement>('[data-testid="plate-confirm"]');
    if (el) { el.focus(); focusedQuote.current = quoteId; }
  }, [pay, p.busy, quoteId]);
  const resultReady = !!(pl && summary && pl.derivation_state !== "pending");
  return (
    <Card data-plate-entry="true" data-mode="background_plate_lock" data-ready={ready ? "true" : "false"} aria-labelledby="plate-title" aria-busy={p.busy || p.loading ? "true" : "false"}>
      <div className={styles.bench}>
        <div className={styles.benchMaterials}>
          <CardHeader>
            <CardTitle>{PLATE_TITLE}</CardTitle>
            <CardDescription>{PLATE_PROMISE}{PLATE_SCOPE_NOTE}</CardDescription>
          </CardHeader>
          <Stepper steps={STEPS} current={stage} label="制作步骤" />
          <div role="status" aria-live="polite" data-testid="plate-status">{p.loading ? "正在读取……" : plateStatusLine(v)}</div>
          {p.loading ? <><Skeleton /><Skeleton style={{ width: "70%" }} /></> : null}
          {p.uploadSlot}
          {p.materialsSlot}
          {!p.loading && mode && !ready ? <Alert tone="info" title="暂时不能使用" data-testid="plate-unavailable">{reasonText(mode.reason)}</Alert> : null}
          {!p.loading && ready && !hasPhoto && !v ? <Alert tone="warn" title="还不能开始" data-testid="plate-no-photo">{NO_USABLE_PHOTO}</Alert> : null}
          {ready && !v && hasPhoto ? (
            <form onSubmit={e => { e.preventDefault(); p.onQuote?.(); }} data-testid="plate-form">
              {supported.length > 1 ? (
                <Field htmlFor="plate-input" label="商品照片" hint="只列出在已验证范围内的照片">
                  <select id="plate-input" value={chosen?.input_id} onChange={e => p.onInput?.(e.target.value)} aria-describedby="plate-input-hint">
                    {supported.map(i => <option key={i.input_id} value={i.input_id}>{KIND_LABEL[i.kind] ?? "样本"} · {i.canvas.width_px}×{i.canvas.height_px}</option>)}
                  </select>
                </Field>
              ) : <p data-testid="plate-photo">商品照片：{KIND_LABEL[chosen?.kind ?? ""] ?? "样本"} · 输出 {chosen?.canvas.width_px}×{chosen?.canvas.height_px}</p>}
              <Field htmlFor="plate-background" label="背景描述" hint="1 到 200 个字，例如：深蓝色渐变摄影棚背景，柔和顶光">
                <textarea id="plate-background" rows={3} maxLength={200} value={p.background ?? ""} onChange={e => p.onBackground?.(e.target.value)} aria-describedby="plate-background-hint" required />
              </Field>
              <p>{PRICING_NOTE}</p>
              <Button type="submit" disabled={!!p.busy || bg.length === 0}>获取报价</Button>
            </form>
          ) : null}
        </div>
        <div className={styles.benchStage}>
          {v ? (
            <section aria-label="报价与费用" data-testid="plate-quote">
              {q ? <p>报价 <strong>{formatMinor(q.amount_minor)}</strong> · 付款人：{v.payment.payer_label} · 有效至 {expiry(q.expires_at_unix)}</p> : <p>报价待核实。</p>}
              {pl?.background_intent ? <p>背景：{pl.background_intent}</p> : null}
              <p>{PRICING_NOTE}</p>
              {pay ? <Button type="button" disabled={!!p.busy} onClick={p.onConfirm} data-testid="plate-confirm">确认并生成 {formatMinor(q?.amount_minor)}</Button> : null}
              {charged(v) ? <p>已扣费 {formatMinor(v.payment.original_charged_minor ?? v.payment.charged_minor)}。</p> : null}
            </section>
          ) : null}
          {contentSrc ? (
            <figure>
              <img className={styles.resultImg} alt={pl?.candidate_state === "limited_candidate" ? "换背景后的商品图" : "换背景后的商品图（未通过检查，不可使用）"} src={contentSrc} width={pl?.canvas?.width_px} height={pl?.canvas?.height_px} />
              <figcaption>
                <span className={styles.stageMeta}>
                  <span className={styles.chip}>主体保真 {summary?.label ?? "待确认"}</span>
                  <span className={styles.chip}>{v?.selected ? "已选定" : "尚未选定"}</span>
                  <span className={styles.chip}>{pl?.canvas?.width_px}×{pl?.canvas?.height_px}</span>
                </span>
              </figcaption>
            </figure>
          ) : (
            <div className={styles.stageEmpty}>
              <p>{p.loading ? "正在读取……" : v && v.confirmed && pl?.derivation_state === "pending" ? "正在生成背景并检查商品……" : "结果会显示在这里。选照片、写背景、确认费用，步骤没有增加。"}</p>
            </div>
          )}
          {resultReady ? (
            <section aria-label="结果与商品检查" data-testid="plate-result" data-candidate={pl!.candidate_state}>
              <Alert tone={summary!.tone === "ok" ? "ok" : summary!.tone === "bad" ? "danger" : "warn"} title={summary!.label}>{summary!.detail}</Alert>
              {failed.length > 0 ? <Alert tone="danger" title="没有通过的检查">{failed.map(a => axisLabel(a.name)).join("、")}</Alert> : null}
              <div className={styles.actions}>
                <Button type="button" disabled={!!p.busy || !canSelectPlate(v!)} onClick={p.onSelect} data-testid="plate-select">{v!.selected ? "已选定" : "选定这张"}</Button>
                {canExportPlate(v!) ? <a className={styles.link} href={sourcePath(p.projectID, v!.run_id, "export")} data-testid="plate-export">导出图片与检查报告</a> : <span aria-disabled="true">导出不可用</span>}
                {pl!.candidate_state === "limited_candidate" ? <a className={styles.link} href="#compare">并排比较</a> : null}
                {pl!.candidate_state === "limited_candidate" ? <a className={styles.link} href="#revision">只改这里</a> : null}
              </div>
            </section>
          ) : null}
        </div>
        <div className={styles.benchRail}>
          {p.railSlot}
          {resultReady ? (
            <>
              {(pl!.axes ?? []).length > 0 ? (
                <>
                  <h3>商品检查（逐项）</h3>
                  <ul className={styles.axisList} aria-label="商品检查结果">
                    {(pl!.axes ?? []).filter(a => a.machine_checked).map(a => <li key={a.name}><span>{axisLabel(a.name)}</span><span className={stateClass(a.state)}>{stateLabel(a.state)}</span></li>)}
                  </ul>
                  <h3>无法由机器检查的项目</h3>
                  <ul className={styles.axisList} aria-label="无法由机器检查的项目">
                    {(pl!.axes ?? []).filter(a => !a.machine_checked).map(a => <li key={a.name}><span>{axisLabel(a.name)}</span><span className={stateClass(a.state)}>{stateLabel(a.state)}</span></li>)}
                  </ul>
                  <p>是否真实好用：尚未经过真人评审。</p>
                </>
              ) : null}
              {(pl!.limits ?? []).length > 0 ? <><h3>使用限制</h3><ul aria-label="使用限制">{(pl!.limits ?? []).map(l => <li key={l}>{l}</li>)}</ul></> : null}
              {p.returnEnabled && pl!.candidate_state === "limited_candidate" ? (
                <div data-testid="plate-return">
                  <Alert tone="warn" title="回传到来源订单 / 活动">{RETURN_NOTE}</Alert>
                  <div className={styles.checkRow}>
                    <input id="plate-return-ack" type="checkbox" checked={!!p.returnAck} onChange={e => p.onReturnAck?.(e.target.checked)} />
                    <label htmlFor="plate-return-ack">我知道下游收到的回执不含上述限制说明</label>
                  </div>
                  <Button type="button" disabled={!!p.busy || !p.returnAck || !canReturnPlate(v!, true)} onClick={p.onReturn}>回传到来源</Button>
                  {!v!.selected ? <p>请先选定这张结果，才能回传。</p> : null}
                  {p.returnNotice ? <p role="status">{p.returnNotice}</p> : null}
                </div>
              ) : null}
            </>
          ) : null}
          {p.notice ? <Alert tone="danger" title="没有完成" data-testid="plate-notice">{p.notice}</Alert> : null}
          <div className={styles.actions}>
            {v ? <Button type="button" variant="outline" disabled={!!p.busy} onClick={p.onRefresh}>刷新状态</Button> : null}
            {v ? <Button type="button" variant="outline" disabled={!!p.busy} onClick={p.onNew}>换个背景再试（重新报价）</Button> : null}
          </div>
          <CapabilityMatrix capabilities={p.capabilities} />
          {v ? (
            <details data-technical="true">
              <summary>技术详情</summary>
              <dl>
                <dt>请求</dt><dd>{v.run_id} · {v.phase}</dd>
                <dt>背景生成任务</dt><dd>{v.task_id || "无"}{v.task_phase ? ` · ${v.task_phase}` : ""}</dd>
                {pl?.plate_task ? <><dt>背景图文件</dt><dd>{pl.plate_task.asset_id} · sha256 {pl.plate_task.sha256}</dd></> : null}
                {pl?.composite_sha256 ? <><dt>合成结果 hash</dt><dd>{pl.composite_sha256}</dd></> : null}
                {pl?.report_sha256 ? <><dt>检查报告 hash</dt><dd>{pl.report_sha256}</dd></> : null}
                {pl?.verdicts ? <><dt>分项结论</dt><dd>fidelity {pl.verdicts.fidelity} · generation {pl.verdicts.generation} · billing {pl.verdicts.billing} · human {pl.verdicts.human_usefulness}</dd></> : null}
                <dt>模型</dt><dd>{v.model} · {v.provider} · {v.size}</dd>
              </dl>
            </details>
          ) : null}
        </div>
      </div>
    </Card>
  );
}

const NOT_OPEN: Array<{ id: string; label: string }> = [
  { id: "creative_background", label: "创意换背景" },
  { id: "light_scene", label: "光影场景" },
  { id: "showcase_video", label: "展示视频" },
  { id: "reference_edit", label: "参考图编辑" },
  { id: "first_image", label: "首图生成（旧版）" },
];

// CapabilityMatrix tells the truth about every mode: two are server driven, the
// rest are closed until they have real success evidence.
export function CapabilityMatrix({ capabilities }: { capabilities: SourceCapabilities | null }) {
  const plateMode = capabilities?.modes.find(m => m.mode === "background_plate_lock");
  const textMode = capabilities?.modes.find(m => m.mode === "text_generate");
  const unknown = capabilities === null;
  const rows = [
    { id: "background_plate_lock", label: "限定保真换背景", state: unknown ? "unknown" : plateMode?.ready ? "available" : "unavailable", note: unknown ? "选好照片后显示" : plateMode?.ready ? "可用范围：冻结样本" : reasonText(plateMode?.reason ?? "") },
    { id: "text_generate", label: "文字生成概念图", state: unknown ? "unknown" : textMode?.ready ? "available" : "unavailable", note: unknown ? "选好照片后显示" : textMode?.ready ? "不保证商品一致" : "暂时不能使用" },
    ...NOT_OPEN.map(m => ({ ...m, state: "not_opened", note: "未开放：还没有真实成功证据" })),
  ];
  return (
    <section aria-label="做图方式" data-testid="capability-matrix">
      <h3>做图方式</h3>
      <ul className={styles.axisList}>
        {rows.map(r => (
          <li key={r.id} data-mode={r.id} data-state={r.state}>
            <span>{r.label}</span>
            <span className={r.state === "available" ? styles.stateOK : styles.stateNA}>{r.note}</span>
          </li>
        ))}
      </ul>
    </section>
  );
}
