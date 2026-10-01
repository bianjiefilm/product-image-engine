# Product image CI

The existing CI Mac runs this repository's dedicated `mac-product-image-engine-01`
runner with `self-hosted, macOS, ARM64` labels. The fleet truth is
`bianjiefilm/pilotseaview`'s `docs/ci-infrastructure-runbook.md` and `docs/ci/fleet/`.
No monitor, alerting agent or production application runs on the client computer.
Registration clones the canonical runner template without registration credentials,
worktrees or logs; only the new runner receives the existing `fleet.env` baseline.
Other runner configurations and services are not changed.

CI runs on main, this feature branch, same-repository PRs to main and manual
dispatch. Fork PRs do not receive private module access or run on this runner.
Checkout does not persist its token. The repository's `MODULE_AUTH_BASIC` secret
supplies a Git HTTP extraheader only to the locked dependency-download step;
the Go module version and go.sum remain authoritative. Missing credentials fail
that step. The secret is not written to global Git config or passed to Web tests.
Private-module authentication does not grant product IAM or billing authority.

Go comes from `server/go.mod`; Web uses Node 22.23.2, matching the fleet's cached
version, and `npm ci` with the committed lockfile. Module cache is runner-local.
After download/checksum verification, server vet/build/full tests and the six
source-consumer race packages run with GOPROXY off. Web runs full Vitest, strict
typecheck and production build. These suites need no additional external binary
or production provider credential. New binary dependencies must follow the
fleet's persistent, atomic, per-runner bootstrap contract in the same PR.

There is no deployment, source-generation flag enablement, account provisioning,
payment or provider call in this workflow. A green run proves this exact checked
out source passed software checks; real server and ordinary-user Web acceptance
remain separate release gates.
