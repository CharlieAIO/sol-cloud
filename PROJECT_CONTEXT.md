# Sol-Cloud Project Context

This file is the shared handoff for future coding agents working in this repo. Read it before changing code. `AGENTS.md` and `CLAUDE.md` intentionally point here so the operational knowledge stays in one place.

## Product Summary

Sol-Cloud is a Go CLI that deploys a hosted Solana local validator (`solana-test-validator`) to cloud providers. It aims to give teams a stable remote RPC and WebSocket endpoint with localnet-like behavior: cloned accounts/programs, runtime limits, startup program deploys, airdrops, persistent ledger storage, status checks, and destroy/restart workflows.

The CLI currently supports:

- Fly.io as the original/default provider.
- Railway as a supported provider with API setup plus `railway up`.
- Local project config through `.sol-cloud.yml`.
- User credentials in the OS config directory through `internal/config/credentials.go`.
- Local deployment state in `.sol-cloud/state.json`.
- Generated deploy artifacts under `.sol-cloud/deployments/<app>/`.

## Current Workspace Context

This workspace has an ignored local `.sol-cloud.yml` for a Railway deployment:

- Provider: `railway`
- App/project name: `sol-cloud-cyoj2zpr`
- Region: `us-west`
- Runtime: `slots_per_epoch=432000`, `ticks_per_slot=64`, `compute_unit_limit=1000000`, `ledger_limit_size=10000`, `ledger_disk_limit_gb=45`
- It contains several cloned accounts/programs and startup airdrops.

Do not assume this local config is tracked in git. `.sol-cloud.yml`, `.sol-cloud/`, `.sol-cloud.local.yml`, and `*.log` are ignored. Generated artifacts are local operational files, not source of truth, unless the user explicitly asks to inspect or regenerate them.

## Repository Layout

- `main.go`: CLI entrypoint; calls `cmd.Execute()`.
- `cmd/`: Cobra commands and user workflows.
- `internal/validator/`: validator runtime config, defaults, and validation.
- `internal/providers/`: provider abstraction, Fly and Railway implementations, deployment helpers, generated template data, command execution wrappers.
- `internal/config/`: credentials and local deployment state persistence.
- `internal/utils/`: interactive prompts and provider-safe name generation.
- `internal/monitor/`: slot progression history used by `sol-cloud watch`.
- `templates/`: embedded deployment templates for Docker, nginx, Fly, and the validator entrypoint.
- `scripts/`: install scripts for Unix and PowerShell.

## Main Dependencies

The project is a Go module. Notable packages:

- `github.com/spf13/cobra` for CLI commands.
- `github.com/spf13/viper` for config/env loading.
- `gopkg.in/yaml.v3` is present in the module graph for YAML handling.

External CLIs needed by some runtime paths:

- `flyctl` for Fly deploys. Fly deploy path uses API for setup but still calls `flyctl deploy --remote-only`.
- `railway` for Railway deploys. Railway setup uses GraphQL API, then calls `railway up`.
- `solana` for `clone-program` and generated container startup program deploys.
- Docker is indirectly needed by Fly remote deploy tooling, depending on local/flyctl behavior.

## Configuration Model

User project config is read by Viper from `.sol-cloud.yml` in the current directory by default. `cmd/root.go` also adds `$HOME/.config/sol-cloud` as a config search path and enables environment overrides with prefix `SOL_CLOUD`; dots and hyphens map to underscores.

Top-level config fields:

- `provider`: `fly` or `railway`; defaults to `fly` in deploy if missing.
- `app_name`: required for deploy.
- `org`: Fly org slug or Railway workspace/team ID depending on provider.
- `region`: provider region; deploy defaults to `ord` for Fly and `us-west` for Railway.
- `validator`: runtime settings from `internal/validator.Config`.

Validator defaults live in `internal/validator/config.go`:

- `slots_per_epoch`: `432000`
- `ticks_per_slot`: `64`
- `compute_unit_limit`: `200000`
- `ledger_limit_size`: `10000`
- `ledger_disk_limit_gb`: `45`
- `clone_rpc_url`: `https://api.mainnet-beta.solana.com`
- `airdrop_accounts`: empty
- `clone_programs`: empty
- legacy `clone_accounts` and `clone_upgradeable_programs`: empty
- `program_deploy`: empty

Important validation behavior:

- Base58 addresses are checked with a simple regex, not RPC validation.
- Duplicate clone and airdrop addresses are rejected within each field.
- `program_deploy` must be all-or-nothing: `.so`, program ID keypair, and upgrade authority keypair are required together.
- The deprecated config key `validator.program_deploy.program_id` is still accepted as a fallback for `program_id_keypair`.

## CLI Commands

### `sol-cloud init`

Implemented in `cmd/init.go`.

- Interactive setup writes `.sol-cloud.yml`.
- `--yes` skips prompts, generates an app name, and uses defaults.
- `--provider railway` with `--yes` chooses Railway defaults.
- Generated config includes `ledger_disk_limit_gb`.
- Interactive runtime customization prompts for slots, ticks, compute unit limit, ledger limit size, and ledger disk limit.
- It collects unified `clone_programs`, optional airdrop accounts, and optional startup program deploy paths.

### `sol-cloud auth fly`

Implemented in `cmd/auth.go`.

- Saves Fly access token and default org slug to user credentials.
- Token can come from `--token` or prompt.
- Org can come from `--org` or prompt, defaulting to saved org or `personal`.
- Verification first probes the Machines API. It falls back to GraphQL for certain 403/404 cases.
- `--skip-verify` saves without API calls.

### `sol-cloud auth railway`

Implemented in `cmd/auth.go`.

- Saves Railway API token and workspace ID.
- Verifies token with `me { id }`, falling back to `projects` for token types that do not allow `me`.
- Attempts workspace discovery via `me.teams`, `me.workspaces`, or a `teamId` from projects.
- If multiple workspaces are found, prompts with arrow selection and falls back to text input.
- If discovery fails, prompts for workspace ID from the Railway dashboard URL.
- `--skip-verify` saves without token verification, but the workspace discovery path still runs best-effort.

### `sol-cloud deploy`

Implemented in `cmd/deploy.go`.

Flow:

1. Resolve provider from config.
2. Validate provider-specific app/project name.
3. Resolve region.
4. Build `validator.Config` from Viper config.
5. Apply defaults, then CLI flag overrides.
6. Validate config.
7. Build `providers.Config`.
8. Instantiate provider with `providers.NewProvider`.
9. Call provider `Deploy`.
10. On non-dry-run success, write/update `.sol-cloud/state.json`.

Important flags:

- `--dry-run`: render artifacts and skip cloud deploy.
- `--skip-health-check`: skip RPC health wait after cloud deploy.
- `--health-timeout`, `--health-interval`
- Runtime overrides: `--slots-per-epoch`, `--ticks-per-slot`, `--compute-unit-limit`, `--ledger-limit-size`, `--ledger-disk-limit-gb`.
- Clone overrides: `--clone-program`, `--clone`, `--clone-upgradeable-program`.
- Startup program deploy overrides: `--program-so`, `--program-id-keypair`, deprecated `--program-id`, `--upgrade-authority`.
- `--reset`: sets `ForceReset`; generated entrypoint clears the existing ledger on startup.
- `--clone-rpc-url`: endpoint used by generated validator startup clone flags.
- Volume flags: `--volume-size`, `--skip-volume`. Fly uses these directly. Railway GraphQL volume creation currently does not support setting volume size in this implementation.

Dry-run renders provider artifacts but does not write deployment state.

### `sol-cloud status`

Implemented in `cmd/status.go`.

- Resolves a deployment from `.sol-cloud/state.json`, defaulting to `LastDeployment`.
- Falls back to an explicit Fly deployment name when not found in state for backward compatibility.
- Calls provider `Status`.
- Separately calls JSON-RPC methods against the recorded RPC URL: `getHealth`, `getSlot`, `getRecentPerformanceSamples`.
- Prints provider state, RPC health, slot, TPS, endpoints, dashboard URL, and provider warning if any.

### `sol-cloud destroy`

Implemented in `cmd/destroy.go`.

- Resolves deployment from local state unless a name is passed.
- Defaults provider to Fly if missing from old state records.
- Prompts unless `--yes`.
- Calls provider `Destroy`.
- Removes deployment from local state after successful cloud destroy.

### `sol-cloud watch`

Implemented in `cmd/watch.go`.

- Watches slot progression by polling `getSlot`.
- Uses `internal/monitor.SlotHistory`.
- Detects stuck validators when slot has not advanced beyond `--stuck-threshold`.
- Can restart through provider `Restart`, either interactively or with `--auto-restart`.
- Has cooldown and max restart controls.

### `sol-cloud clone-program`

Implemented in `cmd/clone.go`.

- Requires local `solana` CLI.
- Dumps a program binary from a source RPC using `solana program dump`.
- Can snapshot additional accounts and optionally the upgradeable programdata account.
- Writes helper account flags under `.sol-cloud/programs/<program>-accounts/validator-account-flags.txt`.
- Optional `--deploy` deploys the dumped binary to a target validator, resolving target RPC from local state if not provided.
- This command is local Solana CLI based; it does not modify `.sol-cloud.yml`.

## Provider Abstraction

`internal/providers/provider.go` defines:

- `Config`: provider-agnostic deploy inputs.
- `Deployment`: endpoints and metadata returned after deploy.
- `Status`: provider status fields.
- `Provider` interface: `Deploy`, `Destroy`, `Status`, `Restart`.
- `validatorTemplateData`: fields passed to embedded templates.
- `NewProvider`: maps `fly` and `railway`.

When adding provider-level config, update every path:

1. `internal/validator.Config` if it is validator runtime behavior.
2. `cmd/deploy.go` Viper read, flag override, output text.
3. `cmd/init.go` generated config and prompts if applicable.
4. `internal/providers/provider.go` template data.
5. Each provider (`fly.go`, `railway.go`) copying config into template data.
6. Relevant templates.
7. README and this context file.

## Fly Provider

Main files:

- `internal/providers/fly.go`
- `internal/providers/fly_api_deploy.go`
- `templates/fly.toml.tmpl`

Deploy behavior:

- Renders Dockerfile, Fly toml, nginx config, and entrypoint under `.sol-cloud/deployments/<app>/`.
- API setup ensures app exists, networking exists, and persistent volume exists unless skipped.
- Uses `flyctl deploy --remote-only --ha=false --wait-timeout=15m --yes`.
- Health check waits for RPC unless skipped.
- Fly URL defaults to `https://<app>.fly.dev` and `wss://<app>.fly.dev`.

Fly volume behavior:

- `--volume-size` is passed to Fly volume creation.
- `--skip-volume` uses ephemeral storage and the generated Fly toml omits the mount.
- Fly template mounts `<app>_ledger` at `/var/lib/solana/ledger`.

Fly credentials:

- Resolved from provider field, env, or saved credentials. See `resolveAccessToken` in `fly.go` if changing this area.
- Default org resolution checks explicit config/flag, `SOL_CLOUD_FLY_ORG`, saved credentials, then `personal`.

## Railway Provider

Main files:

- `internal/providers/railway.go`
- `internal/providers/railway_api_deploy.go`

Deploy behavior:

- Renders Dockerfile, nginx config, and entrypoint. Railway does not use `fly.toml`.
- Uses Railway GraphQL to ensure project, service, environment, volume, and public domain.
- Then calls `railway up --ci --service <serviceID>` with env vars pointing at project/service/environment.
- Creates a project-scoped token for CLI deploy when possible because `railway up` expects project auth. Falls back to account token with a warning if token creation fails.
- Persists `railway-ids.json` in the deployment artifact directory with project ID, service ID, and domain.
- `Status`, `Restart`, and `Destroy` depend on `railway-ids.json`.

Railway project/service resilience:

- If a previous deploy wrote `railway-ids.json`, deploy reuses that project ID to avoid duplicate projects in team workspaces.
- It still calls `ensureRailwayService` so deleted services can be recreated.
- If the saved project ID is not found, deploy recreates the project.

Railway volume behavior:

- The implementation creates/attaches a volume at `/var/lib/solana/ledger`.
- The GraphQL schema used here does not expose a size field for `VolumeCreateInput`; comments note Railway sets default size and resizing is done in the dashboard.
- Existing log text mentions `30 GB`, but Railway deployments may have dashboard-resized volumes. The runtime `ledger_disk_limit_gb` guard is the reliable protection against filling the mounted filesystem.

Railway credentials:

- Token resolution checks explicit provider field, `SOL_CLOUD_RAILWAY_TOKEN`, `RAILWAY_TOKEN`, then saved credentials.
- Workspace ID can come from top-level `org`, saved credentials, or API discovery.

## Generated Runtime Container

Templates:

- `templates/Dockerfile.tmpl`
- `templates/nginx.conf.tmpl`
- `templates/entrypoint.sh.tmpl`

Dockerfile:

- Based on `ubuntu:22.04`.
- Installs `wget`, `curl`, `ca-certificates`, `bash`, `nginx`, `bzip2`.
- Downloads Agave/Solana CLI release `2.3.13` for `x86_64-unknown-linux-gnu`.
- Copies generated nginx config, entrypoint, and optional program assets.
- Exposes port `8080`.

nginx:

- Listens on `8080`.
- `/health` returns `ok`.
- Proxies normal HTTP to Solana RPC on `127.0.0.1:8899`.
- Proxies WebSocket upgrades to `127.0.0.1:8900`.

entrypoint:

- Builds `solana-test-validator` args from template data.
- Uses persistent ledger path `/var/lib/solana/ledger`.
- Preserves existing ledger unless empty, forced reset, or disk cap exceeded.
- Adds `--reset` only when starting from an empty/fresh ledger.
- Always passes `--limit-ledger-size`.
- Adds `--ticks-per-slot` and `--compute-unit-limit` only if the installed validator supports the flags.
- `clone_programs` are handled through `clone_program_auto`, which currently uses `--clone-upgradeable-program` when supported. Legacy `clone_accounts` use `--clone`; legacy `clone_upgradeable_programs` use `--clone-upgradeable-program`.
- Optional startup program deploy waits for local RPC, airdrops SOL to upgrade authority, and runs `solana program deploy`.
- Optional startup airdrops run after the local RPC becomes healthy.

Ledger disk guard:

- `LEDGER_DISK_LIMIT_GB` comes from `validator.ledger_disk_limit_gb`.
- `LEDGER_MONITOR_INTERVAL_SECONDS` defaults to `30`; invalid values are reset to `30`.
- `LEDGER_FILESYSTEM_HEADROOM_PERCENT` defaults to `85`; invalid values or values over `99` are reset to `85`.
- The effective cap is the lower of configured GB and the filesystem-size-derived headroom cap.
- If current ledger usage reaches the cap before startup, the entrypoint clears the ledger and starts fresh.
- During runtime, the entrypoint supervises validator and nginx. If usage reaches the cap, it stops the validator, waits for it, clears the ledger, starts a fresh validator, and reruns startup hooks.
- Clearing uses `find "${LEDGER_DIR:?}" -mindepth 1 -delete` to avoid deleting the volume mount itself.

## Program Deploy Assets

Both providers call `prepareProgramDeployData` before rendering templates. That helper copies configured startup program artifacts into `.sol-cloud/deployments/<app>/program` and returns paths used inside the generated container.

When changing startup program deploy behavior, inspect the helper in `internal/providers` and the generated `entrypoint.sh.tmpl` together. The container expects paths that exist inside `/opt/sol-cloud/program`.

## Credentials and State

Credentials:

- File name: `credentials.json`.
- Directory preference: `SOL_CLOUD_CONFIG_DIR/sol-cloud`, then `$XDG_CONFIG_HOME/sol-cloud`, then OS user config dir plus `sol-cloud`.
- Saved with mode `0600`.
- Contains Fly and Railway credentials. Do not print tokens in logs or commit credentials.

State:

- File path: `.sol-cloud/state.json`.
- Saved with mode `0644`.
- Tracks deployment records: name, provider, RPC URL, WebSocket URL, region, artifact dir, dashboard URL, timestamps.
- `LastDeployment` drives default `status`, `destroy`, `watch`, and `clone-program --deploy` target resolution.

## Local/Generated Files to Treat Carefully

Ignored files commonly present in this workspace:

- `.sol-cloud.yml`: local project config, may include user-specific app and clone settings.
- `.sol-cloud/`: generated deploy artifacts, state, deploy logs, program dumps.
- `.tmp-go-cache`: local Go cache if used.
- `*.log`
- IDE files.

Do not delete, reset, or rewrite these unless the user asks. Generated artifacts can be regenerated with `go run . deploy --dry-run`, but they may be useful for debugging the currently deployed instance.

## Development Workflow

Use these commands from repo root:

```bash
go test ./...
go run . deploy --dry-run
bash -n .sol-cloud/deployments/<app>/entrypoint.sh
```

Notes:

- `go run . deploy --dry-run` renders deployment artifacts using the current local `.sol-cloud.yml`.
- If Go cannot write to the normal build cache in the sandbox, rerun with allowed/escalated permissions instead of changing code.
- For template-only changes, `bash -n` on the rendered entrypoint is important because Go tests do not execute shell templates.
- Use `gofmt -w` on changed Go files.
- Prefer focused tests around changed behavior. The repo currently has no package test files, so `go test ./...` is mostly compile verification.

## Coding Conventions and Pitfalls

- Keep provider behavior behind the `Provider` interface where practical.
- Preserve backward compatibility for old config keys and old state records unless there is a strong reason not to.
- Be careful with provider-specific API changes. Railway GraphQL shapes in particular have changed over time; existing code often tries multiple query shapes for resilience.
- Avoid making Railway deploy depend entirely on generic project listing because team workspace projects may not always be visible through that path. Saved `railway-ids.json` is intentionally reused.
- Do not assume `--limit-ledger-size` alone prevents full disks. The repo has an explicit disk usage guard for persistent volumes.
- Do not delete the ledger mount directory itself; clear contents only.
- Health checks should be optional because newly cloned validators can take time or upstream clone RPC can be flaky.
- Keep log files helpful. Deploy functions write cloud/CLI output to `.sol-cloud/deployments/<app>/deploy.log` when available.
- Avoid leaking tokens. When adding logs around auth/deploy, include resource IDs and statuses, not secret values.
- When adding CLI flags, wire them into config, dry-run output, README, and generated templates as applicable.
- If changing public command behavior, update README and this file.

## Common Troubleshooting Paths

Railway deploy fails:

- Check `.sol-cloud/deployments/<app>/deploy.log`.
- Confirm `railway` CLI exists in PATH.
- Confirm `railway-ids.json` exists after a successful deploy.
- If project/service was deleted from Railway, deploy should recreate missing project/service using saved IDs and workspace info.
- If volume creation fails, deploy logs a warning and continues; runtime persistence may be affected.

Fly deploy fails:

- Check `.sol-cloud/deployments/<app>/deploy.log`.
- Confirm `flyctl` exists in PATH and token/org are valid.
- Volume creation warnings fall back to ephemeral storage.
- Health check failures can be bypassed with `--skip-health-check` if deploy otherwise succeeded.

Validator RPC unhealthy:

- Check provider logs first.
- Confirm nginx is running and proxying port 8080 to local Solana RPC/WS.
- Startup clone RPC may be rate-limited; use `validator.clone_rpc_url` or `--clone-rpc-url` with a private endpoint.
- If the persistent ledger has stale state and startup clone args are ignored, redeploy with `--reset`.

Volume nearing/full:

- Ensure `validator.ledger_disk_limit_gb` is set lower than the provider volume cap.
- Runtime will reset the ledger when the cap is reached; this sacrifices ledger history/state to keep the service running.
- For Railway 50 GB volumes, 45 GB is the intended local cap.

## Release/Install Notes

README documents curl/PowerShell install scripts and `go install github.com/CharlieAIO/sol-cloud@latest`. If changing release layout or binary names, update:

- `README.md`
- `scripts/install.sh`
- `scripts/install.ps1`
- Any release automation under `.github/`

## Agent Operating Guidance

Before making changes:

1. Run `git status --short`.
2. Read the files you plan to change.
3. Treat existing uncommitted changes as user work unless you made them in this session.
4. Prefer `rg`/`rg --files` for search.
5. Keep docs, config structs, CLI flags, templates, and provider copy code in sync.

After making changes:

1. Run `gofmt -w` for changed Go files.
2. Run `go test ./...`.
3. If templates changed, render with `go run . deploy --dry-run`.
4. Syntax-check rendered shell with `bash -n .sol-cloud/deployments/<app>/entrypoint.sh`.
5. Summarize changed files and verification clearly.

