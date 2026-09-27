import type { ButtonHTMLAttributes } from "react";
import styles from "./ui.module.css";

type Variant = "default" | "outline" | "secondary";
type Size = "default" | "sm";

// Button 对齐 shadcn/ui 的 variant/size 接口,样式限定在本模块,避免改写全站按钮。
export function Button({
  variant = "default",
  size = "default",
  className,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant; size?: Size }) {
  const names = [styles.btn, size === "sm" ? styles.sm : "", variant === "outline" ? styles.outline : "", variant === "secondary" ? styles.secondary : "", className]
    .filter(Boolean)
    .join(" ");
  return <button className={names} {...props} />;
}
