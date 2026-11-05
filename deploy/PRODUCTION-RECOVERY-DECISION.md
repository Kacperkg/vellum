# Production recovery evidence decision

**Status: blocked for merge as complete automation and for activation.**
This decision was made against draft PR #47 at `081016f3ac5d2f5aa6947895c1908ba7abaae66a`
and staging at `d55de6b119b2d1836bacd8259fdfbedf9e7da0eb`. The proposed
architecture requires a disposable proof before provisioning or policy changes.
The checked-out workflow defaults to standard production approval. Live
configuration was outside this review.

## Decision

Prefer independent, scheduled and release-triggered recovery jobs with retained
evidence and a short, private issuer invocation. Retain the host-side nonce,
local root-only socket and signature check. Prepare slow work before dispatching
SSM, then issue the short-lived approval once the actual invocation is known.
A missing preparation, unavailable service or changed baseline stops promotion
before writer drain. After durable mutation begins, preserve the existing
pending marker and recovery procedure.

| Option | Trust and failure modes | Operations |
| --- | --- | --- |
| A. Dedicated broker and relay, recovery work on request | Can separate signing and restoration. Running a restore in the SSM request window couples promotion to worker startup, archive transfer, restore and any retries. A single broker outage blocks every promotion. | Requires an available broker, patching and supervision as well as the scheduler, worker, durable evidence and alerts. Existing PR #47 defines the socket protocol and host verifier. |
| B. Managed private ingress, on-demand issuer and disposable workers | Slow jobs prepare signed evidence outside the SSM window. The issuer must independently check current eligibility and live binding for every attempt. Scheduler failure, missing evidence, expired evidence and issuer outage fail closed. | Fewer continuously operated processes. Requires separate roles, an authenticated job/result channel, receipt retention, an independent freshness alarm, and network isolation proven on the selected worker platform. |

Use option B as the design direction. Prefer a small managed control plane
with an IAM-authenticated managed request and response mailbox. The host
relay uses a separate root-protected credential scoped to the mailbox. Keep
request and reply destinations fixed, cap bytes and retries, and consume one
authorisation per attempt. Verify credential issuance and revocation without
granting mailbox access to the shared instance role. The control plane
independently resolves GitHub and SSM facts, manages bounded jobs and signs
the approval. It should never execute the candidate or handle database rows.
A trusted downloader can use a Fargate task with a narrowly scoped read role.
Hand off exact verified inputs to a disposable sandbox that runs PostgreSQL,
`pg_restore` and, where required, the candidate. Treat both restore and candidate
execution as potentially hostile. Keep credentials and the trusted harness
outside the sandbox network and process boundary. A Fargate task role is
available at task scope, so credentialled download and sandbox execution need
separate boundaries. Prove the hand-off and return of redacted, authenticated
results before choosing the final worker and network topology.
[Amazon ECS task roles](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/task-iam-roles.html)

### Trust boundaries

- **GitHub deployment runner.** It can request work and cause denial of
  service with its temporary deployment permissions. It must have no recovery
  object access or signing permission. The issuer uses independently scoped
  read access to check protected `main` and the exact, latest successful
  ordinary staging-push attempt, CI, artefact, manifests, images and bundle.
  A run ID, role session name or SHA supplied by the runner is an assertion.
  Use the issuer’s own repository-scoped GitHub read identity. The current
  staging publisher is part of the trust boundary. Independently
  verify staging SSM and host receipt evidence, and require separate build
  attestations if that publisher is removed from the trust boundary.
- **Production root or Docker daemon.** A compromise can bypass local checks,
  drain writers, fabricate host measurements or misuse the producer credential.
  This design trusts the production host for measurements and archive creation.
  The issuer compares measurements with the independently retained last
  successful production receipt, approved source/runtime/proxy policy and
  matching drill. Compromising root must confer no independent release
  signing capability.
- **Production application container.** Treat shared instance metadata
  credentials as potentially reachable until isolation is verified. The
  checked-in backend also has bootstrap PostgreSQL privileges and can alter
  live schema and data. Authentic backup provenance proves origin rather
  than the integrity of a compromised database. Even with
  immutable S3 objects, a writer can submit an entirely new triple under fresh
  keys. The root-private producer must authenticate the completed triple with a
  distinct pinned producer identity unavailable through instance metadata,
  the Docker socket, or application mounts.
- **Recovery workload and control plane.** A candidate or malicious dump can
  exploit its execution environment. Restore into bounded encrypted scratch
  with no live routes, shared mounts, public ports, production secrets or access
  to either metadata IP. A compromised trusted recovery harness can falsify
  drill results and read the input available to it. Keep its receipt identity
  separate from the release issuer. The issuer validates job origin, pinned
  harness/profile, release and result linkage, and cannot infer truth from
  candidate-supplied output. Compromise of the issuer's signing identity breaks
  the independent evidence boundary.

Use separate producer, drill and release-approval signing domains and
credentials. The root-installed controller pins the issuer public key. A
non-exportable Ed25519 key in KMS is a candidate for the issuer. Verify AWS
`ED25519_SHA_512` RAW signatures against the existing OpenSSL path and enforce
the key ID and canonical encoding in a disposable test. Keep the complete
domain-separated message within KMS’s 4,096-byte RAW limit.
[AWS KMS signing key specs](https://docs.aws.amazon.com/kms/latest/developerguide/symm-asymm-choose-key-spec.html)

## Implementation prerequisites

1. **Source and snapshot.** Pin the production Compose project and service,
   PostgreSQL image and PGDATA volume, primary role, system ID and database OID.
   Resolve the exact running container at measurement time. A physical clone can
   share the latter two values. With explicit local socket, database, user and
   sanitised libpq configuration, start a read-only repeatable-read transaction.
   Record PostgreSQL transaction time as the conservative lower bound, export
   its snapshot and record an upper bound. Keep the transaction alive while
   the dump imports it with `pg_dump --snapshot`. Cancel and verify both
   sessions on failure. Never advance freshness by rechecking an old object.
   Sign the canonical completion record after verified uploads. Bind the bucket
   owner, completion key, source profile, timestamps and tool versions, plus
   the exact manifest and archive keys, non-null version IDs, hashes and sizes.
   The independent verifier records the completion object’s own version ID,
   hash and size after upload and binds them into its receipts. Require the
   same values on bounded HEAD/GET and verify archive bytes before use. Preserve conditional creation on retries
   and enforce it in bucket policy. The current producer requests conditional
   PUT client-side and the existing policy does not require it. A versioned
   cutover needs reviewed lifecycle and no worker delete/write access on backup
   objects. Keep v1 readable for manual restore and freshness during transition.
   Only genuine v2 backups and corresponding drills may qualify for signing.
   [PostgreSQL snapshot export](https://www.postgresql.org/docs/17/functions-admin.html)
   and [S3 conditional writes](https://docs.aws.amazon.com/AmazonS3/latest/userguide/conditional-writes-enforce.html)
2. **Drills and rehearsal.** Schedule at most every six days, with daily checks
   for missing backups and near-expiry, alert on failures and missing runs, and
   exercise scheduler delivery, cleanup and alerts in a disposable environment.
   Verify the source and archive before a real PostgreSQL 17 transactional
   restore. Test catalog, constraints, relationships, sequences and financial
   and auth invariants. Retain signed, redacted receipts for at least 30 days
   separately from archive retention. Bind the original snapshot and immutable
   object versions, archive age at drill start, exact PG image and verification
   profile to the receipt. Pin or explicitly normalise database name, role,
   grants, locale and settings for schema comparison. Record the test runtime
   profile separately, including authorised substitutions such as disabled SES.
3. **Invocation binding and timing.** Require the installed SSM Agent's
   `SSM_COMMAND_ID`, absent from the current request, and independently
   check the actual invocation, target, parameters and AWS-reported numeric
   document version and hash. Correlate the invocation with the independently
   validated protected-main workflow authorisation. A candidate design uses a
   dedicated-audience GitHub OIDC token to obtain a single-use, manifest-bound
   authorisation. The issuer validates token issuer, audience, expiry and replay
   protection, then verifies the exact protected workflow, environment, event,
   ref, SHA and active job attempt. Atomically bind the authorisation to a single
   independently verified command. OIDC claims omit the checkbox value. Keep the
   reviewed GitHub job condition as the checkbox gate. If the job after that gate
   and the command cannot be joined reliably, require independent approval
   evidence before continuing. Keep bearer tokens out of SSM parameters
   and logs. Inventory all executable historical document versions, since a
   root-capable alternate document could bypass the controller. Validate the
   agent path, API eventual consistency and authorisation in a disposable test. Bind
   all of that and the exact manifest and live profile to a new nonce. Current
   runner-side pins and the document ARN restriction do not prove which numeric
   version actually ran. Authorise registry work before pulling candidate images
   and keep remote waits outside shared locks. Use a single monotonic budget
   with measured limits for revalidation and mutation. The current relay can
   wait 420 seconds inside a 480-second preflight window, and the individual
   mutation timeouts can together exceed the nominal 420-second reserve.
   Recheck source, schema, runtime, proxy, bundles, nonce and expiry under the
   existing locks before writer drain. Emit the validated SSM command ID as
   soon as AWS accepts it and verify a redacted receipt for that invocation
   after both running digests and health are checked, release receipts are
   durable, and the pending marker is finalised. Retain the canonical request
   alongside accepted signed evidence to reconstruct the signature binding.
   [GitHub OIDC claims](https://docs.github.com/en/actions/reference/security/oidc),
   [SSM Agent command ID](https://github.com/aws/amazon-ssm-agent/blob/mainline/agent/plugins/runscript/runscript.go)
   [SSM invocation lookup](https://docs.aws.amazon.com/systems-manager/latest/APIReference/API_GetCommandInvocation.html),
   [command parameters](https://docs.aws.amazon.com/systems-manager/latest/APIReference/API_ListCommands.html)
   and [document metadata](https://docs.aws.amazon.com/systems-manager/latest/APIReference/API_DescribeDocument.html).
   Review the broader read visibility of the SSM command lookup identity.
4. **Backend admission.** The current backend starts with the PostgreSQL
   bootstrap credential and runs GORM AutoMigrate plus category seeding every
   time. A fresh restore and equal final hashes cannot detect transient DDL,
   lock hazards, partially committed work or writes conditional on newer rows.
   Establish a reviewed schema-owner/migrator and restricted application role,
   with a migration-free serving entry point that is enforced at cutover.
   Verify ownership, memberships and privileged functions. Exercise a read-only
   startup phase or another reviewed boundary that prevents persistent startup
   writes even when steady-state serving can write rows.
   Classify the exact candidate by pinned migration and schema/data contract
   artefacts and tested image/runtime behaviour. Rehearse changed backend
   execution against the exact recent archive and check that startup cannot
   migrate, that financial/auth behaviour is compatible, and that errors leave
   the right recovery state. Until this boundary exists, changed backend
   execution requires maintenance. Changes to schema, data contracts or the
   trusted controller, SSM or signer stay in separately reviewed maintenance
   when safety is uncertain. Unchanged backend execution and controller bytes
   may qualify once the independent backup and drill gates work.

## London cost envelope

Illustrative usage is five scheduled drills and four candidate rehearsals at
15 minutes each per month (2.25 compute hours), before retries and tax. Rates
below are AWS public USD On-Demand offers for `eu-west-2` checked on 5 November
2026. These are marginal costs and a budgeting envelope, conditional on the
network topology and data volume.

- An always-on Linux `m7i.large` is $0.11655/hour, about **$85.08/month** at
  730 hours. A 20 GB gp3 volume at $0.0928/GB-month is **$1.86/month**.
- A 2-vCPU, 4-GB Linux/x86 Fargate job costs $0.11356/hour at $0.04656 per
  vCPU-hour and $0.00511 per GB-hour. All 2.25 hours would be **$0.26/month**
  in compute. With five Fargate drills and four `m7i.large` rehearsal jobs,
  compute is also about **$0.26/month**, plus ephemeral VM storage. A single
  issuer KMS key adds **$1/month**, plus requests.
- Private connectivity dominates at small volume. A provisioned interface
  endpoint at $0.011/hour is **$8.03/month per endpoint per AZ**, plus data.
  Two in one AZ add **$16.06/month**. An S3 gateway endpoint has no hourly
  charge. A continuously provisioned London NAT gateway adds **$36.50/month
  per AZ** before data and public IP. Count ECR, Logs, KMS, private ingress
  and control-plane paths once the topology is selected, and budget separately
  for receipt storage, alarms, requests, transfer and retries.

[AWS London EC2/EBS offer](https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AmazonEC2/current/eu-west-2/index.json),
[London ECS offer](https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AmazonECS/current/eu-west-2/index.json),
[London VPC offer](https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AmazonVPC/current/eu-west-2/index.json)
and [KMS pricing](https://aws.amazon.com/kms/pricing/).

## Gate to resume implementation and rollout

The immediate proof must establish private relay/issuer authentication and
actual SSM/workflow correlation without giving either deploy or application
credentials recovery authority. In parallel, review the serving/migrator
separation and prove the trusted harness can restore and rehearse the exact
candidate without giving it a route to credentials or archive storage. This draft has no selected, tested
credential and network topology for those boundaries. A successful existing backup drill, signature unit tests or a historical restore
cannot close those gates. A future provisioning plan should use separately
owned infrastructure state for the ingress, scheduler, isolated recovery
network/worker, narrowly scoped identities and keys, retained receipts and
alarm targets. Choose ownership, networking and a cost ceiling before applying.

Complete the producer, worker, issuer and host changes together with the full
synthetic path, including outages, drift, expiry, replay and interrupted
mutation. Then obtain independent security, PostgreSQL and deployment review.
For separately approved activation, install the matching six-file bundle under
the drained maintenance process. Obtain an ordinary protected staging-push
release with that bundle, then establish the matching production receipt via
standard approval. Obtain a genuine v2 backup and qualify a drill against the
resulting production baseline before enabling the automated host policy. Verify
the new numeric AWS SSM version/hash and protected Environment pins before
the separately approved first rollout. Preserve lock inodes, pending records and historical receipts.
Approval-system rollback is a separately reviewed policy transition under a
freeze, with a compatible bundle and fresh standard approval. It never
restores database contents or clears recovery state.
