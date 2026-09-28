import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type { CompletionLevels } from "@/lib/consume";

export type ConsumePanelProps = {
  payerDisplay: string;
  presentation: string;
  generateAllowed: boolean;
  mustRequote: boolean;
  topup?: { entry: string; returnTarget: string; quoteRef: string } | null;
  levels: CompletionLevels;
  onReconfirm?: () => void;
};

export function ConsumePanel(props: ConsumePanelProps) {
  const topup = props.topup;
  return (
    <section data-consume-panel="true" data-generate={props.generateAllowed ? "true" : "false"}>
      <p>
        付款主体 <Badge>{props.payerDisplay}</Badge>
      </p>
      <p>
        报价 <Badge variant="warn">{props.presentation}</Badge>
      </p>
      {props.mustRequote ? (
        <Button type="button" variant="secondary" onClick={props.onReconfirm}>
          重新确认报价
        </Button>
      ) : null}
      {topup ? (
        <p>
          余额不足。请从统一充值入口返回后重新确认报价。
          <a
            href={`/billing-center?entry=${encodeURIComponent(topup.entry)}&return_target=${encodeURIComponent(topup.returnTarget)}&quote_ref=${encodeURIComponent(topup.quoteRef)}`}
          >
            {topup.entry} {topup.returnTarget} {topup.quoteRef}
          </a>
        </p>
      ) : null}
      <p>
        完成等级 {props.levels.implementationReady} / {props.levels.integration} / {props.levels.billing} /{" "}
        {props.levels.channelBilling} / {props.levels.productionAuthorized}
      </p>
    </section>
  );
}
