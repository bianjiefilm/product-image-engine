import type { HTMLAttributes } from "react";
import styles from "./ui.module.css";

export function Badge({
  variant = "secondary",
  className,
  ...props
}: HTMLAttributes<HTMLSpanElement> & { variant?: "secondary" | "warn" }) {
  return (
    <span
      className={[styles.badge, variant === "warn" ? styles.warn : "", className].filter(Boolean).join(" ")}
      {...props}
    />
  );
}
