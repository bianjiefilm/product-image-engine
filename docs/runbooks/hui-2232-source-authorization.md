# Source authorization checkpoint (Task3)

Parent Task2 exact `81dcba11874f728b383aaa695eaecd09beb11fb3` independently
reviewed PASS before this increment. This increment does not register routes,
quote, mint accounts, dispatch, settle, release references or start processes.

Actor contains only verified JWT sub/account. Personal project lookup uses exact
account or `personal/default:<account>` plus matching CreatedBy; no organization
claims, email-derived identity or fallback. A different creator in the same
account is hidden. Personal history works without fabricated org membership.

The narrow store metadata lookup is solely an authorization input. It returns
no grant, uses no cache, changes no rows or LastScope and never feeds a raw HTTP
resource response. Organization authorization immediately requires real current
Context app/user/tenant, known delegation payer and allowed role/member facts.
Foreign/missing/revoked/unresolved/personal-payer/unknown roles all have the same
not-found result. Typed-nil Context fails closed. Write roles are owner/admin/editor
and money membership owner/payer; authorized own-history read additionally allows
member/viewer and payer viewer. Another initiating user's run remains invisible.

Organization read resolution sends no tenant/source selector. An explicit write
may resolve its verified project tenant. No unsupported SourceRef or made-up
source tenant is introduced. Archived authorized project permits reads and denies
writes; deleted projects are hidden. Local 800x800 project dimensions are not
silently rewritten to the ordinary source profile's 1024x1024 result dimensions.

LoadRun compares original app/user/principal account/tenant/project. Read keeps
its original payer even after a current delegation payer changes; writes reject
a changed payer with conflict. No retargeted account, task, amount or quote.

New source and org flags default off. Normal source profile is the fixed reviewed
provider/model/image.generate/quantity1/1024*1024 tuple. Explicit pricing, all four
dedicated distinct app tokens/addresses, matching configured app/identity, download
hosts plus Billing/Generation switches are required to advertise new creation.
Organization additionally requires its flag. Configured hosts are validated by
Task6's fetcher before content can be used; these flags do not prove live SKU,
credit, personal bridge, E7 delegation or provider readiness. Disabled new creation
does not disable the separate authenticated historical LoadRun path.

Actual test-first RED: missing Authorizer/Actor/Service/Profile and four config
fields (prepared before Task2 completion, preserved externally and rerun after
approval); missing NewProfile API for config readiness; typed-nil Context panic
before its guard. Twelve source authorization tests and one config test cover the
above policy, wrong owner/app/tenant/user, historical payer change, archive and
configuration matrix using real temporary product SQLite + explicitly fake Context
port. No fake JWT organization claims or server SQL identity edits.

Fresh normal sourceflow/config passed 1.197s/1.441s; full server suite passed after
settling changes (sourceflow 3.100s, config 1.543s, httpapi 5.833s, platform
1.662s, store 3.294s). Fresh scoped race passed sourceflow 5.550s, config 1.406s,
store 7.165s; scoped vet exited 0. Exact commit is handed to root for independent
review. This is engineering evidence. Organization real E7, Web, Billing,
Production and fidelity/customer adoption acceptance remain incomplete here.
