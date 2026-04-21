# Webhook-triggered Apify scrape

Validated end-to-end against real Apify infrastructure on 2026-04-21. This walk-through builds two buttons and one drawer that:

1. Fire an Apify scrape via POST to Apify's API, register a webhook callback.
2. When Apify finishes, it POSTs to our Cloudflare-tunneled listener.
3. The listener matches the path to a drawer trigger, presses the drawer, which fetches the scraped dataset back from Apify.

Total round-trip: ~15 seconds for one Instagram post.

## Prereqs

- Apify account + API token
- Cloudflare account + zone you own (e.g. `example.com`)
- `cloudflared` on PATH (`brew install cloudflared` on macOS)

## 1. Store the Apify token as a battery

```sh
buttons batteries set APIFY_TOKEN <your-token> --global
```

Stored at `~/.buttons/batteries.json` with 0600 perms, outside any repo. Shell/code buttons read it as `$BUTTONS_BAT_APIFY_TOKEN`.

## 2. Set up the Cloudflare tunnel

```sh
buttons webhook setup --hostname webhook.example.com
```

Opens your browser for `cloudflared tunnel login`, picks the zone, creates a tunnel named `buttons`, routes the hostname. Validates up front that the hostname isn't an apex (use `--allow-apex` if you deliberately want the root) and that `cloudflared` actually created the DNS on the right zone (not a silent suffix-append).

If you'd rather skip the cert.pem flow entirely, use a scoped API token:

```sh
export BUTTONS_CF_API_TOKEN=<your-token>
buttons webhook setup --hostname webhook.example.com
```

Token needs `Account: Cloudflare Tunnel: Edit` + `Zone: DNS: Edit`.

## 3. Build the buttons

### `apify-scrape-ig-post` — fire-and-register

```sh
buttons create apify-scrape-ig-post \
  --runtime shell \
  --arg url:string:required \
  --arg callback_url:string:required \
  --code 'WEBHOOK_JSON=$(printf "%s" "[{\"eventTypes\":[\"ACTOR.RUN.SUCCEEDED\",\"ACTOR.RUN.FAILED\",\"ACTOR.RUN.TIMED_OUT\"],\"requestUrl\":\"$BUTTONS_ARG_CALLBACK_URL\"}]" | base64 | tr -d "\n")
curl -sS -X POST \
  -H "Content-Type: application/json" \
  -d "{\"directUrls\":[\"$BUTTONS_ARG_URL\"],\"resultsType\":\"posts\",\"resultsLimit\":20,\"addParentData\":false}" \
  "https://api.apify.com/v2/acts/shu8hvrXbJbY3Eb9W/runs?token=$BUTTONS_BAT_APIFY_TOKEN&webhooks=$WEBHOOK_JSON"' \
  --description "Kick off Apify Instagram posts scrape with webhook callback" \
  --timeout 30
```

The trick: Apify expects the `webhooks` query parameter to be base64-encoded JSON. We subscribe to the three terminal event types so the callback fires whether the run succeeded, failed, or timed out.

### `apify-fetch-dataset` — pull the results

```sh
buttons create apify-fetch-dataset \
  --runtime shell \
  --arg dataset_id:string:required \
  --code 'curl -sS "https://api.apify.com/v2/datasets/$BUTTONS_ARG_DATASET_ID/items?token=$BUTTONS_BAT_APIFY_TOKEN&clean=true&format=json"' \
  --description "Fetch items from an Apify dataset" \
  --timeout 60
```

## 4. Build the webhook-triggered drawer

```sh
buttons drawer create on-apify-done
buttons drawer on-apify-done add apify-fetch-dataset
buttons drawer on-apify-done set 'apify-fetch-dataset.args.dataset_id=${inputs.webhook.body.resource.defaultDatasetId}'
buttons drawer on-apify-done trigger webhook /apify-done
```

`${inputs.webhook.body.resource.defaultDatasetId}` pulls the dataset ID out of Apify's callback payload. The `webhook` input is auto-declared when a drawer has a webhook trigger.

**Retry policy** (Apify's API occasionally returns 502 under load — verified in testing). Edit `~/.buttons/drawers/on-apify-done/drawer.json` (or the project-local `.buttons/drawers/on-apify-done/drawer.json`) and add an `on_failure` block to the step:

```json
{
  "id": "apify-fetch-dataset",
  "kind": "button",
  "button": "apify-fetch-dataset",
  "args": { "dataset_id": "${inputs.webhook.body.resource.defaultDatasetId}" },
  "on_failure": {
    "action": "retry",
    "max_attempts": 3,
    "backoff": {
      "strategy": "exponential",
      "initial_ms": 2000,
      "factor": 2.0,
      "max_ms": 30000,
      "jitter": true
    }
  }
}
```

The `buttons drawer NAME set` verb doesn't yet accept `on_failure.*` paths — direct JSON edit is the path until that lands.

## 5. Run

**Terminal 1 — listener:**

```sh
buttons webhook listen
```

Prints the route:

```
buttons webhook listen — named tunnel
  listening at: https://webhook.example.com

routes:
  https://webhook.example.com/apify-done  →  on-apify-done
```

Leave it running.

**Terminal 2 — fire the scrape:**

```sh
buttons press apify-scrape-ig-post \
  --arg url=https://www.instagram.com/p/<any-public-post>/ \
  --arg callback_url=https://webhook.example.com/apify-done
```

Apify accepts the run immediately, returns a run envelope with an ID and dataset ID.

**Terminal 1 (~10-30 seconds later):**

```
[serve] drawer on-apify-done ok (312ms)
```

The drawer's full result (including the scraped items) is at `.buttons/drawers/on-apify-done/pressed/<timestamp>.json`.

## Dry-run without the listener

Test the drawer logic without running `webhook listen`:

```sh
buttons drawer on-apify-done press --webhook-body '{"resource":{"defaultDatasetId":"<SOME-DATASET-ID>"},"eventType":"ACTOR.RUN.SUCCEEDED"}'
```

Synthesizes the same `${inputs.webhook.*}` shape a real POST produces. Useful for iterating on drawer step wiring.

## What the plumbing validates

Running this loop exercises (in order):

- `buttons create` with shell runtime + typed args + battery injection
- HTTP-button-free approach for services that need base64-encoded query params
- `buttons webhook setup` → `cloudflared tunnel login` → `tunnel route dns` → stable public URL on your own CF zone
- Auto-rejection of apex hostnames (safety)
- Drift detection when the cert is on a different zone than the hostname (safety)
- `buttons drawer trigger webhook` persistence
- `buttons webhook listen` dispatcher with `cloudflared tunnel cleanup` + process-group kill for stale connectors
- Async drawer press on POST receipt (listener returns 202 immediately, drawer runs in background)
- `${inputs.webhook.body.*}` CEL resolution
- Per-step retry policy with exponential backoff for transient upstream failures
- Drawer run history with full step output
