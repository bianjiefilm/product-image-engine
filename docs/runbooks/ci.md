# Product image CI

The workflow requires a dedicated `mac-product-image-engine-01` runner on the
existing CI Mac with `self-hosted, macOS, ARM64` labels. Registration and a green
GitHub run are separate operational gates; this file does not assert either has
completed. The fleet truth is
`bianjiefilm/pilotseaview`'s `docs/ci-infrastructure-runbook.md` and `docs/ci/fleet/`.
No monitor, alerting agent or production application runs on the client computer.
Registration clones the canonical runner template without registration credentials,
worktrees or logs; only the new runner receives the existing `fleet.env` baseline.
Other runner configurations and services are not changed.

CI runs on main, this feature branch, same-repository PRs to main and manual
dispatch. Fork PRs do not receive private module access or run on this runner.
Checkout does not persist its token. The repository's `PUBLIC_AI_SDK_READ_KEY`
secret must be a dedicated **readonly deploy key registered only on public-ai**.
Only the locked dependency-download step writes it to a private temporary
directory, uses strict pinned GitHub host verification over SSH443, and removes
the directory on shell exit. Git's environment-only URL rewrite matches the
public-ai URL prefix; a similarly prefixed repository name can also be routed to
the same pinned GitHub SSH host. Actual repository access is limited by the
public-ai-only readonly deploy key and locked module/checksum, not by prefix
matching. No client OAuth credential or global Git configuration is
reused. Go module version and go.sum remain authoritative; missing credentials
fail the step. The key is not passed to Web tests.
Private-module authentication does not grant product IAM or billing authority.

Go comes from `server/go.mod`; Web uses Node 22.23.2, matching the fleet's cached
version, and `npm ci` with the committed lockfile. Module cache is runner-local.
As required by fleet runbook §10.3, setup-go explicitly disables actions/cache
and setup-node uses no remote cache; the persistent runner supplies local caches.
After download/checksum verification, server vet/build/full tests and the six
source-consumer race packages run with GOPROXY off. Web runs full Vitest, strict
typecheck and production build. These suites need no additional external binary
or production provider credential. New binary dependencies must follow the
fleet's persistent, atomic, per-runner bootstrap contract in the same PR.

There is no deployment, source-generation flag enablement, account provisioning,
payment or provider call in this workflow. A green run proves this exact checked
out source passed software checks; real server and ordinary-user Web acceptance
remain separate release gates.

The initial workflow lint genuinely failed because job-level env cannot use the
runner context. A follow-up moved cache-path setup after runner assignment.
Independent review then required the remote-cache correction and this runbook's
pending-registration wording. Those failures remain in the campaign evidence.
