# Workflow & Collaboration Guidelines

This repo is built by multiple contributors (including multiple AI agents) working in parallel. To avoid conflicts and confusion, we use **git worktrees** and a few simple rules.

## Goals
- Parallel work without stepping on each other’s changes
- Small, reviewable PRs
- Predictable branch naming and ownership
- Minimal merge conflicts (especially around generated code)

---

## Branching conventions
Use short, descriptive branch names:

- `feature/<epic>-<short>` — new functionality  
  - examples: `feature/e2-vkeys`, `feature/e3-sse-proxy`, `feature/e5-portal-keys`
- `fix/<short>` — bug fixes  
  - example: `fix/sse-flush`
- `docs/<short>` — documentation only  
  - example: `docs/workflow`
- `chore/<short>` — build / tooling / refactors  
  - example: `chore/sqlc-config`

Rules:
- One branch should map to one deliverable (one PR).
- Avoid broad refactors unless explicitly agreed.

---

## Mandatory: use git worktrees
Each contributor/agent must work in their own worktree per task/branch.

### Create a new worktree
From the main repo directory:

```bash
# Create a new branch and worktree
git worktree add ../wt-e3-sse -b feature/e3-sse-proxy

# Move into it
cd ../wt-e3-sse