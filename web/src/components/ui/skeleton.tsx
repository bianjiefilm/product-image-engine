import type { HTMLAttributes } from "react";
import styles from "./ui.module.css";

// Skeleton is decorative loading filler; the live status text carries meaning.
export function Skeleton({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div aria-hidden="true" className={[styles.skeleton, className].filter(Boolean).join(" ")} {...props} />;
}
