import type { ReactNode } from "react";
import styles from "./ui.module.css";

// Field ties a visible label, optional hint and optional error to one control.
// The control must carry id={htmlFor}; hint and error ids are derived from it.
export function Field({ htmlFor, label, hint, error, children }: { htmlFor: string; label: string; hint?: string; error?: string; children: ReactNode }) {
  return (
    <div className={styles.field}>
      <label htmlFor={htmlFor} className={styles.fieldLabel}>{label}</label>
      {children}
      {hint ? <p id={`${htmlFor}-hint`} className={styles.fieldHint}>{hint}</p> : null}
      {error ? <p id={`${htmlFor}-error`} role="alert" className={styles.fieldError}>{error}</p> : null}
    </div>
  );
}
