"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import { ConsumePanel } from "@/components/billing/ConsumePanel";
import { StartScreen } from "@/components/first-image/StartScreen";
import { PhotoUpload } from "@/components/plate-lock/PhotoUpload";
import { TextImagePanel } from "@/components/text-image/Panel";
import { RecoveryStrip } from "@/components/recovery/RecoveryStrip";
import { RevisionRail } from "@/components/revision/RevisionRail";
import { SourceImagePanel } from "@/components/source-image/Panel";
import { Button } from "@/components/ui/button";
import { canGenerate, completionLevels, presentQuote, quoteFingerprint } from "@/lib/consume";
import { firstScreen, PERSONAL_SCOPE, presentResult, resumeTask, statusLabel } from "@/lib/first-image";

type SessionBody = {
  id?: string;
  status?: string;
  output_asset_id?: string;
  quote_label?: string;
  project_id?: string;
  authorized_assets?: string[];
};

type StartResponse = {
  project?: { id?: string; source_type?: string; source_ref?: string; usage_kind?: string; width_px?: number; height_px?: number };
  workspace?: { scope?: string };
  return_href?: string;
  session?: SessionBody;
  authorized_assets?: string[];
};

type QuoteBody = {
  payer_display?: string;
  presentation?: string;
  generate_allowed?: boolean;
  must_requote?: boolean;
  connection?: string;
  settlement?: string;
  funds_state?: string;
  quote_origin?: string;
  quoted?: boolean;
  topup?: { entry?: string; return_target?: string; quote_ref?: string };
};

export default function StartClient() {
  const router = useRouter();
  const search = useSearchParams();
  const screen = firstScreen();
  const source = search.get("source") || "";
  const sourceRef = search.get("ref") || "";
  const sourceTenant = search.get("tenant") || "";
  const returnHref = search.get("return") || "";
  const asset = search.get("asset") || "";
  const topupPage = search.get("topup") || "";
  const quoteRef = search.get("quote_ref") || "";
  const sourced = source === "order" || source === "campaign";
  // A personal project survives a refresh or a re-login through the address: ?project=<id>.
  const projectParam = /^[A-Za-z0-9_-]{1,256}$/.test(search.get("project") ?? "") ? (search.get("project") as string) : "";
  const rechargeReturned = topupPage === "success";

  const [ready, setReady] = useState(false);
  const [scope, setScope] = useState(PERSONAL_SCOPE);
  const [projectId, setProjectId] = useState(projectParam);
  const [taskId, setTaskId] = useState("");
  const [previousTaskId, setPreviousTaskId] = useState("");
  const [status, setStatus] = useState("");
  const [outputAssetId, setOutputAssetId] = useState("");
  const [quoteLabel, setQuoteLabel] = useState("待确认");
  const [assets, setAssets] = useState<string[]>(asset ? [asset] : []);
  const [href, setHref] = useState(returnHref);
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const [reloadKey, setReloadKey] = useState(0);
  const [quotedPayer, setQuotedPayer] = useState("");
  const [quotedPresentation, setQuotedPresentation] = useState("");
  const [quotedGenerate, setQuotedGenerate] = useState(false);
  const [quotedMust, setQuotedMust] = useState(false);
  const [quotedTopup, setQuotedTopup] = useState<{ entry: string; returnTarget: string; quoteRef: string } | null>(null);
  const [connection, setConnection] = useState("");
  const [settlement, setSettlement] = useState("");
  const [fundsState, setFundsState] = useState("");
  const [quoteOrigin, setQuoteOrigin] = useState("");
  const [quotedFlag, setQuotedFlag] = useState(false);
  const [form, setForm] = useState({
    name: "我的产品图",
    usage_kind: "电商主图",
    width_px: 800,
    height_px: 800,
    image_count: 1,
    model: "standard",
    background_direction: "浅灰棚拍",
    photo_asset_id: "",
  });

  const view = presentResult({
    status,
    outputAssetId,
    billingConnected: false,
    quoteLabel,
  });

  const same = useMemo(() => (previousTaskId ? resumeTask(taskId, previousTaskId) : true), [previousTaskId, taskId]);

  function quotePayload(id: string) {
    return {
      project_id: id,
      source_type: sourced ? source : "",
      source_ref: sourceRef,
      source_tenant_id: sourceTenant,
      image_count: form.image_count,
      resolution: `${form.width_px}x${form.height_px}`,
      size: `${form.width_px}x${form.height_px}`,
      model: form.model,
      capability: "image.generate.standard",
      pricing_version: "pricing-2026-09",
    };
  }

  function applyQuote(statusCode: number, body: QuoteBody | null) {
    const missing = statusCode === 503 || body?.connection === "配置缺失";
    if (missing) {
      setConnection("配置缺失");
      setFundsState(body?.funds_state || "unconfigured");
      setNotice("配置缺失");
    }
    if (!body?.payer_display && !missing) return;
    if (body?.payer_display) setQuotedPayer(body.payer_display);
    if (body?.presentation) setQuotedPresentation(body.presentation);
    else if (missing) setQuotedPresentation("配置缺失");
    setQuotedGenerate(body?.generate_allowed === true && !rechargeReturned);
    setQuotedMust(body?.must_requote === true || rechargeReturned);
    if (body?.connection) setConnection(body.connection);
    if (body?.settlement) setSettlement(body.settlement);
    if (body?.funds_state) setFundsState(body.funds_state);
    setQuoteOrigin(body?.quote_origin || "");
    setQuotedFlag(body?.quoted === true);
    const topup = body?.topup;
    setQuotedTopup(
      topup?.entry && topup.return_target && topup.quote_ref
        ? { entry: topup.entry, returnTarget: topup.return_target, quoteRef: topup.quote_ref }
        : null
    );
  }

  async function requestQuote(id: string) {
    const res = await fetch("/api/billing/consume/quote", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(quotePayload(id)),
    });
    const body = (await res.json().catch(() => null)) as QuoteBody | null;
    return { status: res.status, body };
  }

  useEffect(() => {
    let cancelled = false;
    (async () => {
      const session = await fetch("/api/auth/session");
      if (session.status === 401) {
        router.replace("/login");
        return;
      }
      if (session.status === 503) {
        if (!cancelled) {
          setConnection("配置缺失");
          setFundsState("unconfigured");
          setNotice("配置缺失");
          setReady(true);
        }
        return;
      }
      if (sourced && sourceRef && sourceTenant && returnHref) {
        const res = await fetch("/api/entry/source", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            source_type: source,
            source_ref: sourceRef,
            source_tenant_id: sourceTenant,
            return_href: returnHref,
            authorized_assets: asset ? [{ asset_ref: asset, owner_tenant: sourceTenant }] : [],
            usage_kind: "电商主图",
            width_px: 800,
            height_px: 800,
            background_direction: "场景",
          }),
        });
        const data = (await res.json().catch(() => null)) as StartResponse | null;
        if (!cancelled) {
          if (!res.ok) {
            setNotice((data as { error?: { message?: string } } | null)?.error?.message ?? "来源入口失败");
          } else {
            applyStart(data);
          }
          setReady(true);
        }
        return;
      }
      const listed = await fetch("/api/entry/personal");
      const list = (await listed.json().catch(() => null)) as { sessions?: SessionBody[] } | null;
      const current = list?.sessions?.[0];
      if (!cancelled && current?.project_id && !projectParam) {
        setProjectId(current.project_id);
        setScope(PERSONAL_SCOPE);
        const task = await fetch(`/api/projects/${current.project_id}/first-image`);
        if (task.ok) {
          const body = (await task.json()) as { session?: SessionBody };
          applySession(body.session);
        }
      }
      if (!cancelled) setReady(true);
    })();
    return () => {
      cancelled = true;
    };
    // 深链参数变化才重新恢复；表单输入不重新创建工程。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sourced, source, sourceRef, sourceTenant, returnHref, asset]);

  function applySession(session?: SessionBody) {
    if (!session?.id) return;
    setPreviousTaskId((prev) => (taskId && taskId !== session.id ? taskId : prev));
    setTaskId(session.id);
    setStatus(session.status || "");
    setOutputAssetId(session.output_asset_id || "");
    setQuoteLabel(session.quote_label || "待确认");
    if (session.project_id) setProjectId(session.project_id);
  }

  function applyStart(data: StartResponse | null) {
    if (!data) return;
    if (data.workspace?.scope) setScope(data.workspace.scope);
    if (data.project?.id) setProjectId(data.project.id);
    if (data.return_href) setHref(data.return_href);
    if (data.session?.authorized_assets?.length) setAssets(data.session.authorized_assets);
    applySession(data.session);
  }

  async function postFirstImage(id: string) {
    setBusy(true);
    setNotice("");
    try {
      const res = await fetch(`/api/projects/${id}/first-image`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          usage_kind: form.usage_kind,
          width_px: form.width_px,
          height_px: form.height_px,
          background_direction: form.background_direction,
          input_asset_id: form.photo_asset_id,
        }),
      });
      const data = (await res.json().catch(() => null)) as { session?: SessionBody; error?: { message?: string } } | null;
      if (!res.ok) {
        setNotice(data?.error?.message ?? "生成没有开始");
        return;
      }
      applySession(data?.session);
    } finally {
      setBusy(false);
    }
  }

  async function generate(id = projectId) {
    if (!id || rechargeReturned || quotedTopup) return;
    await postFirstImage(id);
  }

  useEffect(() => {
    setQuotedGenerate(false);
    // The legacy quote belongs to the order/campaign deep link only. The personal
    // path prices through the formal source quote inside the plate-lock panel.
    if (!projectId || !sourced) return;
    let cancelled = false;
    (async () => {
      const quote = await requestQuote(projectId);
      if (cancelled) return;
      applyQuote(quote.status, quote.body);
    })();
    return () => {
      cancelled = true;
    };
    // 模型、张数或尺寸变化后必须重新报价。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectId, form.width_px, form.height_px, form.image_count, form.model, source, sourceRef, sourceTenant, sourced]);

  async function refreshTask() {
    if (!projectId) return;
    setBusy(true);
    try {
      const res = await fetch(`/api/projects/${projectId}/first-image/refresh`, { method: "POST" });
      const data = (await res.json().catch(() => null)) as { session?: SessionBody; error?: { message?: string } } | null;
      if (!res.ok) {
        setNotice(data?.error?.message ?? "暂时不能刷新状态");
        return;
      }
      applySession(data?.session);
    } finally {
      setBusy(false);
    }
  }

  const sourceLabel = sourced ? `${source === "order" ? "订单" : "活动"} ${sourceRef}` : "";
  const payerDisplay = sourced ? "待确认" : "个人付款";
  useEffect(() => {
    if (!rechargeReturned || !projectId) return;
    let cancelled = false;
    (async () => {
      await fetch("/api/billing/consume/return", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ project_id: projectId, page: "success" }),
      });
      if (!cancelled) setQuotedGenerate(false);
    })();
    return () => {
      cancelled = true;
    };
  }, [rechargeReturned, projectId]);
  const fingerprint = quoteFingerprint({
    payerAccountRef: sourced ? sourceTenant || "organization" : "personal",
    imageCount: form.image_count,
    resolution: `${form.width_px}x${form.height_px}`,
    capability: "image.generate.standard",
    pricingVersion: "pricing-2026-09",
    model: form.model,
    size: `${form.width_px}x${form.height_px}`,
  });
  const quoteDecision = canGenerate({
    confirmedFingerprint: "",
    currentFingerprint: fingerprint,
    entitled: false,
    fundsShort: false,
    priced: false,
  });
  const heldForFunds = rechargeReturned || quotedTopup != null;

  const personal = !sourced;
  return (
    <StartScreen
      title={screen.title}
      showsCatalog={screen.showsCatalog}
      scope={scope}
      steps={sourced ? screen.steps.slice(1) : screen.steps}
      status={status}
      statusLabel={statusLabel(status)}
      outputAssetId={outputAssetId}
      showImage={view.showImage}
      quoteLabel={view.quoteLabel}
      usefulProven={view.usefulProven}
      fidelityLabel={view.fidelityLabel}
      headline={view.headline}
      uploadRequired={!sourced}
      taskId={taskId}
      sourceLabel={sourceLabel}
      returnHref={href || undefined}
      authorizedAssets={assets}
      compact={personal}
    >
      {personal ? (
        <SourceImagePanel
          mode="background_plate_lock"
          projectId={projectId || undefined}
          reloadKey={reloadKey}
          railSlot={
            <>
              <RevisionRail projectId={projectId || undefined} />
              <RecoveryStrip projectId={projectId || undefined} />
            </>
          }
          uploadSlot={
            <PhotoUpload
              projectId={projectId || undefined}
              onAttached={(id) => {
                setProjectId(id);
                setReloadKey((k) => k + 1);
                try {
                  const url = new URL(window.location.href);
                  url.searchParams.set("project", id);
                  window.history.replaceState(null, "", url);
                } catch {
                  // The address is a convenience for refresh; the project itself is already saved.
                }
              }}
            />
          }
        />
      ) : (
        <>
        <div className="media-actions">
          {projectId ? (
            <Button type="button" onClick={() => generate()} disabled={busy || heldForFunds}>
              用已授权素材生成
            </Button>
          ) : null}
          {projectId ? (
            <Button type="button" variant="outline" onClick={refreshTask} disabled={busy}>
              刷新同一任务
            </Button>
          ) : null}
        </div>
        <ConsumePanel
          payerDisplay={quotedPayer || payerDisplay}
          presentation={quotedPresentation || presentQuote({})}
          generateAllowed={rechargeReturned ? false : quotedGenerate}
          onReconfirm={() => {
            if (!projectId || rechargeReturned) return;
            void fetch("/api/billing/consume/confirm", {
              method: "POST",
              headers: { "Content-Type": "application/json" },
              body: JSON.stringify(quotePayload(projectId)),
            })
              .then(async (res) => ({ status: res.status, body: (await res.json()) as QuoteBody }))
              .then((quote) => {
                applyQuote(quote.status, quote.body);
                if (quote.status === 402 || quote.body?.topup) setQuotedGenerate(false);
              })
              .catch(() => setQuotedGenerate(false));
          }}
          mustRequote={quotedMust || quoteDecision.mustRequote || rechargeReturned}
          topup={
            rechargeReturned
              ? {
                  entry: "billing_center",
                  returnTarget: "ti-product-image-engine-web",
                  quoteRef: quoteRef || quotedTopup?.quoteRef || "待重新报价",
                }
              : quotedTopup
          }
          levels={completionLevels()}
          connection={connection}
          settlement={settlement}
          fundsState={fundsState}
          quoteOrigin={quoteOrigin}
          quoted={quotedFlag}
        />
        <RevisionRail projectId={projectId || undefined} />
        <RecoveryStrip projectId={projectId || undefined} />
        </>
      )}
      {!same ? <p>刷新后的任务编号变了，请不要把它当成同一次生成。</p> : null}
      {notice ? <p role="alert">{notice}</p> : null}
      {personal ? (
        <details>
          <summary>普通文字生成（不保证商品一致）</summary>
          <SourceImagePanel projectId={projectId || undefined} />
          <TextImagePanel projectId={projectId || undefined} historyOnly />
        </details>
      ) : (
        <>
          <SourceImagePanel projectId={projectId || undefined} />
          <TextImagePanel projectId={projectId || undefined} historyOnly />
        </>
      )}
      <p>
        <Link href="/projects">已有制作工程</Link>
      </p>
    </StartScreen>
  );
}
