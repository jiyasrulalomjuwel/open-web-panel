# OWP-0044 — owp_admin DB user missing on socket-auth installs
- **Severity**: Low | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: install/stages.sh socket-auth path created tenants but not the shared owp_admin user the panel needs.
- **Solution**: stages.sh now also creates owp_admin (CREATE USER + GRANT + FLUSH) over the socket before rollback registration.
- **Files**: install/stages.sh
- **Verify**: bash -n passes
- **Keywords**: install, owp_admin, socket, mysql
