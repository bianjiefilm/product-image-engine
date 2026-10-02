import type { HTMLAttributes, ReactNode } from "react";
import styles from "./ui.module.css";

type Tone = "info" | "ok" | "warn" | "danger";

// Alert: danger and warn interrupt (role=alert); info and ok are polite status.
export function Alert({ tone = "info", title, children, ...props }: { tone?: Tone; title?: string; children: ReactNode } & Omit<HTMLAttributes<HTMLDivElement>, "title">) {
  const role = tone === "danger" || tone === "warn" ? "alert" : "status";
  return (
    <div role={role} className={[styles.alert, styles[`alert_${tone}`]].join(" ")} data-tone={tone} {...props}>
      {title ? <strong className={styles.alertTitle}>{title}</strong> : null}
      <div>{children}</div>
    </div>
  );
}
