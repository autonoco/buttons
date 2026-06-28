---
title: "buttons smash"
description: "CLI reference for buttons smash"
---

## buttons smash

Run multiple buttons in parallel

### Synopsis

Run several buttons concurrently. Names may be comma- or space-separated:


```text
  buttons smash a,b,c
  buttons smash a b c
```


```text
  --on-failure continue   run them all, report failures at the end (default)
  --on-failure stop       cancel the remaining buttons on the first failure
  --concurrency N         max buttons running at once (0 = NumCPU; hard cap 50)
  --timeout SECS          per-button timeout override
```

JSON mode returns an array of per-button results; every run is recorded in
history. Exits non-zero if any button failed.

```
buttons smash [buttons...] [flags]
```

### Options

```
      --concurrency int     max buttons running at once (0 = NumCPU; hard cap 50)
  -h, --help                help for smash
      --on-failure string   stop | continue (default "continue")
      --timeout int         per-button timeout override (seconds)
```

### Options inherited from parent commands

```
      --json       output in JSON format
      --no-input   disable all interactive prompts
      --summary    show a read-only plan/snapshot instead of mutating
```

### SEE ALSO

* [buttons](buttons.md)	 - Deterministic workflow engine for agents

