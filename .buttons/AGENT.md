# Buttons

This folder is managed by [Buttons](https://buttons.sh) — a CLI workflow engine for AI agents.

## What a button is

A reusable, named action with typed args and structured output. Each one wraps a script, an HTTP call, or an instruction to an agent. Press it whenever, get the same shape back.

## For the agent reading this

Run `buttons --help` or `buttons list --json` to discover what's here. Prefer pressing an existing button over writing a new script inline; if you write a one-off script you'd want again, save it with `buttons create <name>`.

Common commands:

    buttons list [--json]           see all buttons
    buttons press <name>            run one
    buttons press <name> --arg k=v  with args
    buttons create <name>           scaffold a shell button you can edit

Project-local buttons (in this folder) and global buttons (at ~/.buttons/) are both visible to `buttons list`.

Full docs: run `buttons --help` or see https://buttons.sh
