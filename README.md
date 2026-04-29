# Sol-Cloud ☁️

Localnet freedom, globally available.

## Why Sol-Cloud 🎯

Sol-Cloud gives you the flexibility of `solana-test-validator`, but hosted on Fly.io so your team can connect from anywhere.

- Configure validator behavior like localnet (clones, limits, program deploys)
- Share one stable RPC + WebSocket URL with teammates
- Spin environments up and down with a simple CLI flow

Start in 3 steps:

- `sol-cloud` for the interactive menu, or:
- `sol-cloud init`
- `sol-cloud auth fly`
- `sol-cloud deploy`

The CLI uses terminal-native progress bars for long-running work, compact
aligned summaries for results, and arrow-key selectors for provider, region,
and workspace choices. When output is redirected, progress degrades to plain
append-only status lines.

## Install

### macOS / Linux (curl)

```bash
curl -fsSL https://raw.githubusercontent.com/CharlieAIO/sol-cloud/main/scripts/install.sh | sh
```

Install a specific release:

```bash
curl -fsSL https://raw.githubusercontent.com/CharlieAIO/sol-cloud/main/scripts/install.sh | sh -s -- --version v0.1.0
```

### Windows (PowerShell)

```powershell
irm https://raw.githubusercontent.com/CharlieAIO/sol-cloud/main/scripts/install.ps1 | iex
```

Install a specific release:

```powershell
iwr https://raw.githubusercontent.com/CharlieAIO/sol-cloud/main/scripts/install.ps1 -OutFile install.ps1
.\install.ps1 -Version v0.1.0
```

### Manual download

- https://github.com/CharlieAIO/sol-cloud/releases/latest

### Build from source (optional)

```bash
go install github.com/CharlieAIO/sol-cloud@latest
```

Maintainers: Homebrew tap automation setup is in `docs/HOMEBREW_TAP_SETUP.md`.

## Quick start 🚀

```bash
# 1) Create hidden project config
sol-cloud init

# 2) Connect Fly (personal or org token)
sol-cloud auth fly --org personal

# 3) Deploy
sol-cloud deploy

# 4) Check status
sol-cloud status

# 5) Destroy when done
sol-cloud destroy --yes
```

## Core commands

```bash
sol-cloud init
sol-cloud auth fly
sol-cloud deploy
sol-cloud status
sol-cloud destroy --yes
sol-cloud clone-program <program-id> --deploy  # requires local Solana CLI
```

## Useful deploy flags

- `--dry-run`
- `--region`
- `--org`
- `--skip-health-check`
- `--slots-per-epoch`
- `--ticks-per-slot`
- `--compute-unit-limit`
- `--ledger-limit-size`
- `--ledger-disk-limit-gb`
- `--clone` (repeatable)
- `--clone-upgradeable-program` (repeatable)
- `--program-so`
- `--program-id-keypair`
- `--upgrade-authority`

## Config

`sol-cloud init` writes a hidden per-project config outside your repository,
under the Sol-Cloud user config directory. The exact path is printed after init.
Use `--config <path>` when you intentionally want to point at a specific file.
Old local `.sol-cloud.yml` files are still read as a compatibility fallback, but
new configs are not created in the working tree.

```yaml
provider: fly
app_name: "sol-cloud-1a2b3c4d"
region: "ord"
org: "personal"
validator:
  slots_per_epoch: 432000
  ticks_per_slot: 64
  compute_unit_limit: 200000
  ledger_limit_size: 10000
  ledger_disk_limit_gb: 45
  clone_accounts: []
  clone_upgradeable_programs: []
  program_deploy:
    so_path: ""
    program_id_keypair: ""
    upgrade_authority: ""
```

`ledger_disk_limit_gb` guards the persistent ledger volume. The generated
container clears and restarts the local validator ledger when usage reaches the
cap, clamped to 85% of the mounted filesystem so smaller volumes stay protected.

## Logs and state

- hidden project config under the Sol-Cloud user config directory
- `.sol-cloud/state.json` records deployments and the latest long-running
  deploy operation state (`running`, `succeeded`, or `failed`)
- `.sol-cloud/deployments/<app>/deploy.log`

## Troubleshooting 🔧

### Token verify fails

```bash
sol-cloud auth fly --skip-verify
```

### Deploy fails or times out

Read:

- `.sol-cloud/deployments/<app>/deploy.log`

Then retry:

```bash
sol-cloud deploy --skip-health-check
```
