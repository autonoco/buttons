# Drawer example: Apify → Snowflake

End-to-end recipe for an agent composing a drawer that scrapes data
with an Apify actor and upserts the result into a Snowflake table.

The drawer chains two buttons:

1. **`apify-run`** — calls Apify's `run-sync-get-dataset-items` endpoint
   and returns the scraped dataset as JSON.
2. **`snowflake-insert`** — runs a MERGE statement against a Snowflake
   warehouse using the rows produced by `apify-run`.

All commands below are agent-friendly: `--json` output, structured
errors, no interactive prompts.

## 1. Create the buttons

```shell
# HTTP POST to Apify; token is a secret so the CLI redacts it from
# traces.
buttons create apify-run \
  --url 'https://api.apify.com/v2/acts/{{actor_id}}/run-sync-get-dataset-items' \
  --method POST \
  --arg actor_id:string:required \
  --arg token:string:required:secret \
  --arg input:string:required \
  --json

# Shell button wrapping a local Snowflake insert script.
buttons create snowflake-insert \
  --runtime shell \
  --code-file ./snowflake-insert.sh \
  --arg account:string:required \
  --arg user:string:required \
  --arg password:string:required:secret \
  --arg database:string:required \
  --arg table:string:required \
  --arg rows:string:required \
  --json
```

## 2. Create the drawer

```shell
buttons drawer create apify-to-snowflake --json
```

## 3. Add both buttons as steps

```shell
buttons drawer apify-to-snowflake add apify-run snowflake-insert --json
```

## 4. Connect the output of `apify-run` into `snowflake-insert.args.rows`

The auto-match form fails here because there's no `rows` field in
`apify-run`'s output (it returns `body`), so we use the explicit form:

```shell
buttons drawer apify-to-snowflake connect \
  apify-run.output.body to snowflake-insert.args.rows --json
```

## 5. Dry-run first (--summary)

Always safe — no side effects, no network:

```shell
buttons drawer apify-to-snowflake press \
  actor_id=apify/web-scraper \
  input='{"startUrls":["https://example.com"]}' \
  token="$APIFY_TOKEN" \
  account="$SNOWFLAKE_ACCOUNT" \
  user="$SNOWFLAKE_USER" \
  password="$SNOWFLAKE_PASSWORD" \
  database=autono \
  table=apify_runs \
  --summary --json
```

The `--summary` response shows resolved args per step plus a
validation block. Fix any reported errors before pressing for real.

## 6. Press it

```shell
buttons drawer apify-to-snowflake press \
  actor_id=apify/web-scraper \
  input='{"startUrls":["https://example.com"]}' \
  token="$APIFY_TOKEN" \
  account="$SNOWFLAKE_ACCOUNT" \
  user="$SNOWFLAKE_USER" \
  password="$SNOWFLAKE_PASSWORD" \
  database=autono \
  table=apify_runs \
  --json
```

On success the response includes both steps' structured output and a
`run_id` you can cross-reference in the drawer's history.

## 7. Inspect

```shell
# Drawer topology + recent runs + validation
buttons drawer apify-to-snowflake --json

# Workspace snapshot
buttons summary --json
```

## Notes

- **Secrets:** any arg declared with `:secret` is redacted from the
  drawer's persisted trace (`~/.buttons/drawers/apify-to-snowflake/pressed/*.json`).
  The plaintext value still flows to the button at execute time; only
  the trace is protected.
- **Retries:** add `on_failure` to any step by editing its JSON
  directly; v1 honors `action: stop|continue|retry` plus
  `max_attempts` and `backoff`.
- **Schema validation:** the drawer's shape is validated against
  `drawer.schema.json` (embedded + published to SchemaStore). Your
  editor gets autocomplete for free when the file declares `$schema`.
