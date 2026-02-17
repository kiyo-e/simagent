# simagent Troubleshooting

## Common Errors

- `NO_BOOTED_DEVICE`
  - Cause: no booted simulator found while target resolved to `booted`.
  - Action: boot a simulator or pass explicit `--target <UDID>`.

- `TARGET_NOT_FOUND`
  - Cause: provided UDID is unknown.
  - Action: run `./simagent target list --json` and retry with exact UDID.

- `NO_DEFAULT_TARGET`
  - Cause: `target show` used before setting default.
  - Action: run `./simagent target set booted --json` or set explicit UDID.

- `IDB_NOT_FOUND`
  - Cause: `idb` command unavailable in PATH.
  - Action: install/configure `idb`; avoid `ui` and `frame --ui` until fixed.

- `NO_LAST_FRAME`
  - Cause: `ui` command requires cached `elements/transform` but no previous frame exists.
  - Action: run `./simagent frame --json` first, or pass `--from <elements.json>`.

- `ELEMENT_NOT_FOUND`
  - Cause: `--index` or `--id` does not exist in current `elements.json`.
  - Action: re-run `frame`, inspect latest elements, and retry with valid selector.

- `TYPE_FOCUS_FAILED`
  - Cause: `ui type --into ...` could not verify target focus after retries.
  - Action: run `frame`, confirm selector, then retry with explicit selector (`--index` or `--id`) and `--focus-retries`.

- `TYPE_INCOMPLETE`
  - Cause: underlying `idb ui text` dropped part of the input and auto-completion could not fully recover.
  - Action: prefer `ui type --into <selector> --replace --ascii`, then verify with a fresh `frame`.

- `COORD_TRANSFORM_FAILED`
  - Cause: pixel-to-point conversion requested with invalid/missing transform scale.
  - Action: refresh frame and ensure matching `transform.json` is available.

- `SIMCTL_FAILED` / `IDB_UI_FAILED` / `RAW_FAILED`
  - Cause: underlying tool invocation failed.
  - Action: rerun once with a narrow command, inspect error details, then correct target/arguments/tool state.

## Recovery Pattern

1. Re-resolve target with `target list` and explicit `--target` if needed.
2. Re-capture state with `frame --json`.
3. Retry a single minimal action (`ui tap --index ...`, `app launch ...`).
4. If still failing, inspect passthrough output with `raw simctl` or `raw idb`.
