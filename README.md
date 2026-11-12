# artcoded-cli

The artcoded backoffice in your terminal: invoices, expenses, timesheets, dossiers, documents, templates…
A single binary (Go, no dependency), for Linux, macOS and Windows.

```sh
artcoded-cli                    # interactive menu: the one configured in the backoffice settings
artcoded-cli expenses add ~/Downloads/receipt.pdf
artcoded-cli invoices download  # no id: choose the invoice in a list
artcoded-cli timesheets log 8   # 8 hours today
```

## Install

```sh
make build          # ./artcoded-cli
make install        # into $GOBIN (~/go/bin)
make dist           # every platform in dist/
```

Shell completion: `source <(artcoded-cli completion bash)` (or `zsh`).

## First run

The first run asks the configuration and logs in; it is saved **only when the login works**, to
`~/.config/artcoded-cli.yml` (permissions 600). Run `artcoded-cli init` to change it.

```
Backoffice url (to read its keycloak settings, optional): https://backoffice.example.org
✓ found the keycloak settings of the backoffice
Backend url [https://api.example.org]:
Keycloak url [https://auth.example.org]:
Keycloak realm [Artcoded]:
Keycloak client id [artcoded-cli]:
Login method: 1) password  2) browser  3) device
Username: …
Password: …
✓ logged in as nordine (ADMIN), configuration saved to ~/.config/artcoded-cli.yml
```

Giving the backoffice url fills the rest from its runtime config (`/assets/config/config.json`).
Tokens are cached (`~/.cache/artcoded-cli/`) and refreshed; the password is only used when the
session expired.

Without a terminal: `artcoded-cli init --backend-url … --keycloak-url … --realm … --client-id … --username … --password …`.

### Login methods

| method     | how                                                  | keycloak needs                                      |
|------------|------------------------------------------------------|-----------------------------------------------------|
| `password` | username + password of the config (default)          | *Direct access grants* on the client                |
| `browser`  | opens the keycloak login page, comes back on `127.0.0.1` | *Standard flow* + redirect uri `http://127.0.0.1:53682/*` |
| `device`   | shows a code to confirm in any browser (ssh, servers) | *OAuth 2.0 Device Authorization Grant*              |

With 2FA (OTP) on the account, use `browser` or `device`.
Instead of a plain text password: `password_command: "pass show artcoded"`.

## Keycloak: one client to add

The backoffice client (`frontend-client`) only allows the browser flow with the backoffice urls.
Add a client for the cli (the backend accepts it as is: it checks the realm roles, not the client):

1. *Clients → Create client*: client type **OpenID Connect**, client id **`artcoded-cli`**.
2. *Capability config*: **Client authentication: off** (public client), then check
   - **Standard flow** (login method `browser`)
   - **Direct access grants** (login method `password`)
   - **OAuth 2.0 Device Authorization Grant** (login method `device`)

   Only the ones you use are needed.
3. *Login settings*: **Valid redirect URIs**: `http://127.0.0.1:53682/*` (browser login; another port: set
   `redirect_port` in the config). Web origins: none.
4. Nothing else: the realm roles (`ADMIN`, `REGULATOR_OR_ACCOUNTANT`) are in the token by default
   (*roles* client scope).

The errors of keycloak come with a hint (e.g. *enable 'Direct access grants' on the client*).

## Usage

```
artcoded-cli <module> <action> [arguments] [--flags]
artcoded-cli <module>          choose an action
artcoded-cli help <module>     actions of a module
```

**Missing arguments are asked** in a terminal: ids are chosen in a list (type text to filter), files
are asked one path per line (drag & drop works), long texts open `$EDITOR`. Without a terminal (scripts,
cron) a missing argument is an error naming the flag.

Common flags: `--json` (raw json), `--yes`/`-y` (no questions, confirms), `--out <file|dir>`,
`--open` (open what is downloaded), `--stdout`, `--page n --size n`, `--verbose`.

The interactive menu shows the entries of *Settings → Menu* of the backoffice that are visible for you,
in the same order (`admin menu-toggle` changes them), plus the backoffice pages the backend menu doesn't
list yet (templates, toolbox).

| module | what |
|---|---|
| `home` | dashboard, todos, post-its, memo dates, notifications |
| `tasks` | reminders and scheduled actions (with their parameters), results, cron check |
| `timesheets` | `log` hours of a day, rates, settings, close / reopen, generate the invoice, pdf, stats |
| `expenses` | `add` from files (+ tag and price), tag, price, attach / detach, download, labels, exceptional expenses |
| `invoices` | list, guided `new` from a client, edit, pdf / peppol xml, manual upload, attachments, credit note, peppol validate / send, delete / restore |
| `dossiers` | active dossier, add / remove expenses, invoices, documents, close, recall, zip, summary / report / annual report pdf, import |
| `documents` | `upload` a file with title / tags, edit, download |
| `reports` | activity reports: write (markdown in the editor), attachments, post-its, messages, pdf |
| `gallery` | images of the public gallery |
| `cv` | show, edit (json), pdf, download requests |
| `finance` | portfolios with live quotes, symbols, search |
| `uploads` | every file: upload, download (zip), `search` inside files (full text index) |
| `sparql` | query the triplestore (table, json, csv, turtle…) |
| `prospects` | messages of the contact form |
| `clients` | billable clients, rates, contract status, documents |
| `mails` | send mails with files from paths, queue, retry |
| `templates` | `edit` a jinja template in `$EDITOR` with `preview` (mock data), add / replace / default |
| `admin` | company info (logo, signature…), menu, caches, scripts, sms, health, logs |
| `toolbox` | pdf split / rotate, rdf conversion, shacl, xpath |

Entities without a dedicated form (cv, an invoice before generation, a client, a task…) can be edited as
json in `$EDITOR` with `edit`: unknown fields are kept.

### Examples

```sh
# an expense from two files, tagged and priced
artcoded-cli expenses add ticket.pdf receipt.jpg --subject "Train Brussels-Paris" --tag TRAVEL --price 120 --vat 0

# put the tagged expenses of the month in the active dossier and close it
artcoded-cli dossiers add-expenses        # choose: 1,3-7 or all
artcoded-cli dossiers close

# a document
artcoded-cli documents upload ~/scan/statuts.pdf --tags legal,company

# the invoice of the month
artcoded-cli timesheets close && artcoded-cli timesheets invoice
artcoded-cli invoices download --open

# edit the invoice template, preview it with mock data, save it
artcoded-cli templates edit
artcoded-cli templates mock <id> > ctx.json      # change it, then:
artcoded-cli templates preview <id> --file draft.html --context ctx.json --open

# scripts
artcoded-cli invoices list --json | jq '.content[].total'
```

## Configuration

`~/.config/artcoded-cli.yml` (`--config` or `ARTCODED_CONFIG` for another file). Every key can be
overridden with an environment variable `ARTCODED_<KEY>` (e.g. `ARTCODED_PASSWORD`).

```yaml
backend_url: "https://api.example.org"
frontend_url: "https://backoffice.example.org"
keycloak_url: "https://auth.example.org"
realm: "Artcoded"
client_id: "artcoded-cli"
login_method: "password"
username: "nordine"
password: "…"                 # or password_command: "pass show artcoded"
redirect_port: "53682"        # browser login
sparql_endpoint: "https://api.example.org/sparql"
editor: "code --wait"         # default $VISUAL, $EDITOR, nano, vi
download_dir: "~/Downloads"   # default: the current directory
```

`artcoded-cli config [show|edit|set <key> <value>]`.

## Development

```sh
go test ./...   # fake keycloak + backend: init, the three logins, menu, uploads, downloads, previews
```

The commands are in `mod_*.go`, one file per module; the routes are the ones of the backoffice
(`src/api/*.ts`).
