import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

export type StartScreenProps = {
  title: string;
  showsCatalog: boolean;
  scope: string;
  steps: string[];
  status: string;
  statusLabel: string;
  outputAssetId: string;
  showImage: boolean;
  quoteLabel: string;
  usefulProven: boolean;
  fidelityLabel: string;
  headline: string;
  uploadRequired: boolean;
  taskId: string;
  sourceLabel?: string;
  returnHref?: string;
  authorizedAssets?: string[];
  children?: React.ReactNode;
};

export function StartScreen(props: StartScreenProps) {
  const assets = props.authorizedAssets ?? [];
  return (
    <Card
      data-entry-screen="start"
      data-catalog={props.showsCatalog ? "true" : "false"}
      data-scope={props.scope}
      data-task-id={props.taskId}
      data-task-status={props.status}
      data-fake-output="false"
      data-useful-proven={props.usefulProven ? "true" : "false"}
      data-upload-required={props.uploadRequired ? "true" : "false"}
      data-quote={props.quoteLabel}
    >
      <CardHeader>
        <CardTitle>{props.title}</CardTitle>
        <CardDescription>
          上传商品照片，选定场景和背景方向，确认费用后再生成。这里不是生态目录。
        </CardDescription>
      </CardHeader>
      <p>
        <Badge>范围 {props.scope}</Badge>{" "}
        <Badge variant="warn">{props.quoteLabel}</Badge>{" "}
        <Badge>{props.statusLabel}</Badge>
      </p>
      <ol>
        {props.steps.map((step) => (
          <li key={step}>{step}</li>
        ))}
      </ol>
      {props.sourceLabel ? <p>来源 {props.sourceLabel}</p> : null}
      {assets.length > 0 ? (
        <ul>
          {assets.map((asset) => (
            <li key={asset}>已授权素材 {asset}</li>
          ))}
        </ul>
      ) : null}
      {props.returnHref ? <a href={props.returnHref}>返回来源 {props.returnHref}</a> : null}
      <p data-fidelity={props.fidelityLabel}>主体保真 {props.fidelityLabel}</p>
      <p>{props.headline}</p>
      {props.showImage && props.outputAssetId ? <p>候选引用 {props.outputAssetId}</p> : null}
      {props.children}
      <Button type="button" variant="secondary" disabled>
        {props.uploadRequired ? "选择商品照片" : "已带入素材"}
      </Button>
    </Card>
  );
}
