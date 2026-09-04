# Issue Memory

Persistent regression memory for OpenWebPanel. Each solved bug is recorded here so
future changes can be checked against previously fixed issues.

## Usage

- Before editing code, search `issues.json` for related historical issues.
- After a task, recheck all active issues to confirm no regression was reintroduced.
- Never delete issue history automatically.
- Only record bugs that have been verified AND fixed.

## Structure

- `issues.json` — index of all issues and their current status.
- `issue-NNNN.md` — full detail for each solved bug.
- `archive/` — superseded records.
- `reports/` — generated regression reports.