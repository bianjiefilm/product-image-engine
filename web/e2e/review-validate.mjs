// Validates a SIMULATED-HUMAN review written by an independent reviewer agent.
// A review is advisory evidence only: it can never carry a verdict, and it must
// not claim to be a real person.
import fs from "node:fs";

const AXES = ["background_extra_objects", "background_intent_followed", "visual_composite_quality", "transmission_edge"];
const OPINIONS = ["looks_acceptable", "has_concerns", "cannot_tell"];

export function validateReview(doc) {
  const problems = [];
  if (doc?.schema !== "review.simulated_human.v1") problems.push("schema must be review.simulated_human.v1");
  if (doc?.kind !== "SIMULATED-HUMAN") problems.push("kind must be exactly SIMULATED-HUMAN");
  if (doc?.reviewer_context !== "fresh") problems.push("reviewer_context must be fresh (an independent, new context)");
  if (!Array.isArray(doc?.items) || doc.items.length === 0) problems.push("items must be a non-empty array");
  for (const [i, it] of (doc?.items ?? []).entries()) {
    if (!/^[a-z0-9_-]+-[0-9]+x[0-9]+$/.test(it?.case_id ?? "")) problems.push(`items[${i}].case_id`);
    if (!/^[0-9a-f]{64}$/.test(it?.composite_sha256 ?? "")) problems.push(`items[${i}].composite_sha256`);
    for (const a of Object.keys(it?.axes ?? {})) if (!AXES.includes(a) || !OPINIONS.includes(it.axes[a].opinion)) problems.push(`items[${i}].axes.${a}`);
    if (JSON.stringify(it).match(/"(verdict|pass|passed|approved|human_usefulness)"/i)) problems.push(`items[${i}] carries a verdict-like field`);
  }
  if (/real (human|person|user)|真人(已)?(确认|验证|采用)/i.test(JSON.stringify(doc))) problems.push("must not claim a real person reviewed it");
  return problems;
}

if (process.argv[1] && import.meta.url === new URL(`file://${process.argv[1]}`).href) {
  const problems = validateReview(JSON.parse(fs.readFileSync(process.argv[2], "utf8")));
  console.log(problems.length ? `INVALID\n- ${problems.join("\n- ")}` : "VALID (SIMULATED-HUMAN, advisory only)");
  process.exit(problems.length ? 1 : 0);
}
