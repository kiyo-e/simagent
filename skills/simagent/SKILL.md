---
name: simagent
description: Operate the local simagent macOS CLI for iOS Simulator automation with frame capture, indexed UI interaction, and app control. Use when tasks require simulator debugging loops, parsing `elements.json` and `transform.json`, driving UI via tap/type/swipe/button, or invoking simulator and app actions through `xcrun simctl` and `idb`.
---

# Simagent

## Overview

Use this skill to run `simagent` quickly and deterministically during iOS Simulator debugging. Prefer structured JSON outputs, reuse last-frame artifacts correctly, and choose the smallest command that satisfies the task.

## Workflow

1. Validate environment.
- Confirm `xcrun simctl` is available.
- Confirm `idb` is installed when using `frame --ui` or any `ui` command.
- Confirm at least one booted simulator exists (or set explicit target).

2. Prime target selection.
- List targets with `./simagent target list --json`.
- Persist a default target with `./simagent target set booted --json` when repeated calls are expected.

3. Capture a frame before UI actions.
- Run `./simagent frame --json`.
- Read `outDir`, `artifacts.elements`, and `artifacts.transform`.
- Treat element indexes as frame-local; refresh after UI changes.

4. Execute UI actions with explicit targeting.
- Use index or ID when available (`--index`, `--id`) to avoid coordinate drift.
- Use coordinate tap only when no reliable element metadata exists.
- For pixel coordinates, supply `--unit px` and ensure `transform.json` is resolvable from last frame or `--from`.
- For text input, prefer `ui type --into --index|--id|--label --replace --ascii`.
- If input must be exact (phone/OTP/postal), verify value from a fresh frame after typing.

5. Re-capture and continue loop.
- After each meaningful UI action, call `frame` again.
- Recompute decisions from the new `elements.json`.

## Command Selection

- Use `target` for listing/selecting simulator devices and persisted default target.
- Use `frame` for screenshot + UI tree normalization + annotation artifacts.
- Use `ui` for interaction (`tap`, `type`, `clear`, `swipe`, `wait`, `button`, `flow run`).
- Use `app` for simulator-level app actions (`openurl`, `launch`, `terminate`, `list`).
- Use `raw` only for passthrough commands that are not covered by higher-level commands.

## State and Paths

- Default config path: `~/.config/simagent/config.json`.
- Last frame cache path: `~/.config/simagent/last_frame.json`.
- Default frame output path: `${TMPDIR}/simagent/<timestamp>/`.
- `ui` commands without `--from` load `elements` and `transform` from `last_frame.json`.

## Guardrails

- Prefer `--json` whenever the response will be parsed or chained.
- Avoid stale indexes: never assume an index from an older frame remains valid.
- Choose one interaction mode per command: index, ID, or explicit coordinates.
- For unstable text fields, do not use bare `ui type` first; use `--into` so partial-input recovery can target the focused field.
- Pass `--args` marker for `app launch` runtime args, for example `app launch --bundle-id com.example --args --foo bar`.
- Keep failure handling strict: surface command error code/message, then perform the smallest corrective retry.

## References

- Use `references/cli-recipes.md` for copy-ready command patterns.
- Use `references/troubleshooting.md` for common error codes and recovery actions.
