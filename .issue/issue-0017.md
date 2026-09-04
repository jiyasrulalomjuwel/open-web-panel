# OWP-0017 — Backup restore allows SQL executable-comment bypass and -uroot fallback
- **Severity**: Critical | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: mysqldump output may contain /*!...*/ executable comments that carry privileged SQL; a no-tenant-user DB fell back to mysql -uroot (server-level auth, untrusted DB could run privileged statements).
- **Solution**: Restore now rejects any SQL line containing /*! before span handling; restore without a tenant-scoped mysql user is skipped with a server-side log instead of -uroot fallback.
- **Files**: cmd/parentd/backups.go
- **Verify**: go build passes
- **Keywords**: backup, restore, mysql, -uroot, executable-comment
