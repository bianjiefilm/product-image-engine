import type { HTMLAttributes, ReactNode } from "react";
import styles from "./ui.module.css";

export function Card({ className, ...props }: HTMLAttributes<HTMLElement>) {
  return <section className={[styles.card, className].filter(Boolean).join(" ")} {...props} />;
}

export function CardHeader({ children }: { children: ReactNode }) {
  return <header style={{ marginBottom: 12 }}>{children}</header>;
}

export function CardTitle({ children }: { children: ReactNode }) {
  return <h2 className={styles.title}>{children}</h2>;
}

export function CardDescription({ children }: { children: ReactNode }) {
  return <p className={styles.desc}>{children}</p>;
}
