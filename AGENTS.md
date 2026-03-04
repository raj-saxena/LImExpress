# Agents

Agents playing different roles are defined in [./agents](./agents)

Agents are expected to update their own files to capture their learnings and best practices, and to refer to them when working on their tasks. This is a living document and should evolve as we learn more about what works and what doesn't in our specific context.

## Cross-agent working agreements

### Interfaces and ownership
	•	Backend defines API contracts first (OpenAPI or simple markdown specs).
	•	Frontend builds against stable endpoints; no “UI-driven API design.”
	•	Analytics defines event schemas early so backend logs/metrics are consistent from day 1.

### Coding conventions
	•	One package per concern; avoid circular dependencies
	•	Keep request structs and validation close to handlers
	•	No generic repository pattern unless it’s clearly reducing duplication
	•	Prefer plain SQL via sqlc over ORMs

### Review gates (must pass before “MVP done”)
	•	Streaming smoke test passes behind Istio
	•	Budgets enforced for user+team with “smallest remaining wins”
	•	Multi-org isolation tests pass
	•	Logs verified metadata-only
	•	Dashboards show last 90 days aggregated usage (even if simple tables)

