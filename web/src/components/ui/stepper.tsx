import styles from "./ui.module.css";

// Stepper is an ordered list; the current step carries aria-current="step".
export function Stepper({ steps, current, label }: { steps: readonly string[]; current: number; label: string }) {
  return (
    <ol className={styles.stepper} aria-label={label}>
      {steps.map((step, i) => (
        <li key={step} className={[styles.step, i < current ? styles.stepDone : "", i === current ? styles.stepNow : ""].filter(Boolean).join(" ")} aria-current={i === current ? "step" : undefined}>
          <span className={styles.stepNo} aria-hidden="true">{i + 1}</span>
          <span>{step}</span>
          {i < current ? <span className={styles.srOnly}>（已完成）</span> : null}
        </li>
      ))}
    </ol>
  );
}
