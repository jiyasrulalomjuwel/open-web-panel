# OWP-0018 — php-fpm www pool clashes with parentd on 127.0.0.1:9000 in docker
- **Severity**: Critical | **Status**: Resolved | **Date**: 2026-08-10
- **Root cause**: Alpine php-fpm www.conf default pool binds the same fastcgi port parentd expects, causing collision/refused connections after container restart.
- **Solution**: entrypoint.sh removes /etc/php83/php-fpm.d/www.conf before starting php-fpm83 -R so only the panel's pool remains.
- **Files**: deploy/docker/entrypoint.sh
- **Verify**: bash -n passes
- **Keywords**: php-fpm, docker, www pool, 9000
