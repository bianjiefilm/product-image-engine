// Append-only charge ledger. A paid click is only allowed after its `intent`
// line exists; the matching `observed` line records what the platform reported.
// Supplier cost is only ever a DECLARED value: it is never presented as audited.
import fs from "node:fs";
import path from "node:path";

export function openLedger(file) {
  fs.mkdirSync(path.dirname(file), { recursive: true });
  const append = (entry) => fs.appendFileSync(file, JSON.stringify({ at: new Date().toISOString(), ...entry }) + "\n");
  return {
    file,
    intent: (label, fields) => append({ phase: "intent", label, ...fields }),
    observed: (label, fields) => append({ phase: "observed", label, supplier_expense_ledger_verified: false, supplier_cost_basis: "declared_only", ...fields }),
  };
}

export const readLedger = (file) => (fs.existsSync(file) ? fs.readFileSync(file, "utf8").split("\n").filter(Boolean).map((l) => JSON.parse(l)) : []);

// ledgerProblems returns human-readable defects: an observation with no earlier
// intent, an intent that was never observed (the outcome is then UNKNOWN), or
// two intents with one label.
export function ledgerProblems(entries) {
  const problems = [], intents = new Map();
  entries.forEach((e, i) => {
    if (e.phase === "intent") { if (intents.has(e.label)) problems.push(`duplicate intent for ${e.label}`); intents.set(e.label, i); }
    else if (e.phase === "observed") { if (!intents.has(e.label) || intents.get(e.label) > i) problems.push(`observation without a prior intent: ${e.label}`); intents.set(`${e.label}#observed`, i); }
  });
  for (const e of entries) if (e.phase === "intent" && !intents.has(`${e.label}#observed`)) problems.push(`intent never observed (outcome UNKNOWN): ${e.label}`);
  return problems;
}
export const spentMinor = (entries) => entries.filter((e) => e.phase === "observed" && e.payment_status === "charged").reduce((n, e) => n + Number(e.charged_minor ?? 0), 0);
