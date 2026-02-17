# Codex Execution Plans (ExecPlans) for simagent

This document defines how to write and maintain an `ExecPlan` in this repository. An ExecPlan must be usable by a first-time contributor with only the working tree and the plan file.

Reference: https://developers.openai.com/cookbook/articles/codex_exec_plans/

## When an ExecPlan is required

Create an ExecPlan before implementation for large features, significant refactors, behavior-changing CLI updates, JSON contract changes, or any multi-step task likely to span sessions.

You can skip ExecPlan for trivial edits (typos, comments, obvious one-line fixes).

## Non-negotiable requirements

Every ExecPlan must be self-contained, outcome-focused, and maintained as a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` are mandatory and must be updated continuously as work proceeds.

Write for a novice reader. Define non-obvious terms in plain language. Do not assume prior plans or external context.

## Formatting rules

Use prose-first writing. Prefer short paragraphs over long bullet lists. Checklists are allowed only in `Progress`, where checkbox items are required.

Use explicit repository-relative file paths, exact working directories, and exact commands. For command results, include short expected output snippets.

When an `.md` file contains only one ExecPlan, do not wrap the entire file in triple backticks.

## Required section order for each ExecPlan

Use this exact heading order:

1. `# <Short, action-oriented description>`
2. `## Purpose / Big Picture`
3. `## Progress`
4. `## Surprises & Discoveries`
5. `## Decision Log`
6. `## Outcomes & Retrospective`
7. `## Context and Orientation`
8. `## Plan of Work`
9. `## Concrete Steps`
10. `## Validation and Acceptance`
11. `## Idempotence and Recovery`
12. `## Artifacts and Notes`
13. `## Interfaces and Dependencies`

At the top of each plan, include one sentence noting that the plan must be maintained in accordance with `.agent/PLANS.md`.

## simagent project defaults

In `Concrete Steps` and `Validation and Acceptance`, prefer these defaults unless the task requires otherwise:

- Working directory: repository root (`/Users/k.endo/workspace/tries/2026-02-17-simagent`)
- Formatting: `gofmt -w <changed-go-files>`
- Tests: `go test ./...`
- Build: `go build -o simagent .`
- Manual behavior check when relevant: `./simagent frame --target booted --json`

If any step is skipped, state why and how confidence was established.

## ExecPlan template

Copy this skeleton when creating a new plan:

```md
# <Short, action-oriented description>

This ExecPlan is a living document and must be maintained in accordance with `.agent/PLANS.md`.

## Purpose / Big Picture

<Describe user-visible value and how to observe it working.>

## Progress

- [ ] (YYYY-MM-DD HH:MMZ) <Planned step>
- [ ] (YYYY-MM-DD HH:MMZ) <Planned step>

## Surprises & Discoveries

- Observation: <Unexpected behavior or insight>
  Evidence: <Short transcript, test output, or artifact>

## Decision Log

- Decision: <What changed>
  Rationale: <Why this path was chosen>
  Date/Author: <YYYY-MM-DD, name>

## Outcomes & Retrospective

<Summarize delivered behavior, remaining gaps, and lessons learned.>

## Context and Orientation

<Explain current state for a novice; name key files and modules by full path.>

## Plan of Work

<Describe the sequence of edits in prose, including exact file locations and intended changes.>

## Concrete Steps

<State exact commands, working directory, and short expected outputs.>

## Validation and Acceptance

<Describe end-to-end verification with observable inputs/outputs and test expectations.>

## Idempotence and Recovery

<Explain safe re-run behavior, retries, and rollback approach for risky steps.>

## Artifacts and Notes

<Include concise transcripts, diffs, or snippets that prove progress.>

## Interfaces and Dependencies

<Name required modules/libraries, key interfaces/functions, and expected final signatures.>
```
