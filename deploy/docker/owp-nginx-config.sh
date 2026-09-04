#!/bin/sh
# OpenWebPanel Container Nginx Config Generator
# Scans /home/user/ and creates Nginx vhost configs for each domain directory.
# Also creates a catch-all default from public_html.
set -e

NGINX_CONF_DIR=/etc/nginx/http.d
HOME_BASE=/home/user

# Remove old auto-generated configs
find "$NGINX_CONF_DIR" -name 'owp-*.conf' -delete

# Remove Alpine default.conf to prevent duplicate default_server
if [ -f "$NGINX_CONF_DIR/default.conf" ]; then
    rm -f "$NGINX_CONF_DIR/default.conf"
    echo "[OWP] Removed Alpine default.conf"
fi

# Create vhost for each domain directory under home
for dir in "$HOME_BASE"/*/; do
    [ -d "$dir" ] || continue
    domain=$(basename "$dir")

    # Skip hidden dirs, public_html, .owp, .trash
    case "$domain" in
        .*|public_html|owp|trash) continue ;;
    esac

    cat > "$NGINX_CONF_DIR/owp-$domain.conf" << EOF
server {
    listen 80;
    server_name $domain www.$domain;
    root $dir;
    index index.html index.htm index.php;

    access_log /var/log/nginx/$domain.access.log;
    error_log /var/log/nginx/$domain.error.log;

    location / {
        try_files \$uri \$uri/ /index.php?\$args;
    }

    location ~ \.php\$ {
        fastcgi_pass 127.0.0.1:9000;
        fastcgi_index index.php;
        fastcgi_param SCRIPT_FILENAME \$document_root\$fastcgi_script_name;
        include fastcgi_params;
    }

    location ~ /\. {
        deny all;
    }

    location ~ /\.owp {
        deny all;
    }
}
EOF
    echo "[OWP] Created vhost for domain: $domain"
done

# Default catch-all from public_html
if [ -d "$HOME_BASE/public_html" ]; then
    cat > "$NGINX_CONF_DIR/owp-default.conf" << EOF
server {
    listen 80 default_server;
    server_name _;
    root $HOME_BASE/public_html;
    index index.html index.htm index.php;

    location / {
        try_files \$uri \$uri/ /index.php?\$args;
    }

    location ~ \.php\$ {
        fastcgi_pass 127.0.0.1:9000;
        fastcgi_index index.php;
        fastcgi_param SCRIPT_FILENAME \$document_root\$fastcgi_script_name;
        include fastcgi_params;
    }

    location ~ /\. {
        deny all;
    }

    location ~ /\.owp {
        deny all;
    }
}
EOF
    echo "[OWP] Created default vhost from public_html"
fi
