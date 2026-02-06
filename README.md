# Sol-Cloud ☁️

Deploy a shared Solana test validator to Fly.io in minutes.

## Why this exists 🎯

`solana-test-validator` is great locally, but teams need one stable endpoint everyone can use.

Sol-Cloud gives you:

- One setup flow (`init`)
- One auth flow (`auth fly`)
- One deploy command (`deploy`)
- One URL for RPC + WebSocket

## Install

### [Homebrew](https://brew.sh/)

```bash
brew tap CharlieAIO/sol-cloud
brew install sol-cloud
```

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

## Quick start 🚀

```bash
# 1) Create .sol-cloud.yml
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
- `--clone` (repeatable)
- `--clone-upgradeable-program` (repeatable)
- `--program-so`
- `--program-id-keypair`
- `--upgrade-authority`

## Config (`.sol-cloud.yml`)

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
  clone_accounts: []
  clone_upgradeable_programs: []
  program_deploy:
    so_path: ""
    program_id_keypair: ""
    upgrade_authority: ""
```

## Logs and state

- `.sol-cloud.yml`
- `.sol-cloud/state.json`
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
