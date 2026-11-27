#!/usr/bin/env bash
# Runs against throwaway containers on the current local Docker daemon.
set -eu

cd "$(dirname "${BASH_SOURCE[0]}")/.."

docker_host="${DOCKER_HOST:-$(docker context inspect --format '{{.Endpoints.docker.Host}}')}"
case "$docker_host" in
  unix://*) ;;
  *) printf '%s\n' 'The WordPress fixture requires a local Unix Docker daemon.' >&2; exit 1 ;;
esac
docker info >/dev/null

fixture="artex-wp-$(openssl rand -hex 5)"
network=""
database=""
wordpress=""
db_password="$(openssl rand -hex 18)"
admin_password="$(openssl rand -hex 18)"
wp_site=https://wordpress.example.test
wp_user=artex-fixture-editor
wp_image="${SEOOS_WP_IMAGE:-wordpress:php8.3-apache}"
cli_image="${SEOOS_WP_CLI_IMAGE:-wordpress:cli-php8.3}"
mariadb_image="${SEOOS_WP_DATABASE_IMAGE:-mariadb:11.4}"
wp_extra="define('WP_ENVIRONMENT_TYPE','local'); define('DISALLOW_FILE_MODS',true);"

cleanup() {
  if [ -n "$wordpress" ]; then docker rm -fv "$wordpress" >/dev/null 2>&1 || true; fi
  if [ -n "$database" ]; then docker rm -fv "$database" >/dev/null 2>&1 || true; fi
  if [ -n "$network" ]; then docker network rm "$network" >/dev/null 2>&1 || true; fi
}
trap cleanup EXIT

docker network create "$fixture" >/dev/null
network="$fixture"
database="$(docker run -d --name "$fixture-db" --network "$network" \
  -e MARIADB_RANDOM_ROOT_PASSWORD=1 -e MARIADB_DATABASE=wordpress \
  -e MARIADB_USER=wordpress -e "MARIADB_PASSWORD=$db_password" "$mariadb_image")"
ready=false
for _ in $(seq 1 60); do
  if docker exec "$database" healthcheck.sh --connect --innodb_initialized >/dev/null 2>&1; then
    ready=true
    break
  fi
  sleep 1
done
if [ "$ready" != true ]; then printf '%s\n' 'WordPress fixture database did not start.' >&2; exit 1; fi

wordpress="$(docker run -d --name "$fixture-site" --network "$network" \
  -p 127.0.0.1::80 -e "WORDPRESS_DB_HOST=$fixture-db:3306" \
  -e WORDPRESS_DB_USER=wordpress -e "WORDPRESS_DB_PASSWORD=$db_password" \
  -e WORDPRESS_DB_NAME=wordpress -e "WORDPRESS_CONFIG_EXTRA=$wp_extra" "$wp_image")"
wp_address="$(docker port "$wordpress" 80/tcp)"
ready=false
for _ in $(seq 1 60); do
  if curl --fail --silent --max-time 2 "http://$wp_address/wp-login.php" >/dev/null; then
    ready=true
    break
  fi
  sleep 1
done
if [ "$ready" != true ]; then printf '%s\n' 'WordPress fixture HTTP server did not start.' >&2; exit 1; fi

wp() {
  docker run --rm --network "$network" --volumes-from "$wordpress" --user 33:33 \
    -e "WORDPRESS_DB_HOST=$fixture-db:3306" -e WORDPRESS_DB_USER=wordpress \
    -e "WORDPRESS_DB_PASSWORD=$db_password" -e WORDPRESS_DB_NAME=wordpress \
    -e "WORDPRESS_CONFIG_EXTRA=$wp_extra" "$cli_image" --path=/var/www/html --url="$wp_site" "$@"
}

wp core install --title='ARTEX disposable CMS' --admin_user=fixture-admin \
  --admin_email=fixture-admin@example.test --admin_password="$admin_password" --skip-email >/dev/null
wp rewrite structure '/%postname%/' --hard >/dev/null
wp user create "$wp_user" fixture-editor@example.test --role=editor --porcelain >/dev/null
wp term create post_tag 'Fixture taxonomy' --slug=fixture-taxonomy --porcelain >/dev/null
wp_app_password="$(wp user application-password create "$wp_user" artex-integration-test --porcelain)"
wp_app_password="${wp_app_password//[[:space:]]/}"
if [ "${#wp_app_password}" != 24 ]; then
  printf '%s\n' 'Could not create the disposable WordPress Application Password.' >&2
  exit 1
fi
printf 'Started disposable WordPress %s with an editor Application Password.\n' "$(wp core version)"

SEOOS_WP_TEST_HTTP="http://$wp_address" SEOOS_WP_TEST_USER="$wp_user" SEOOS_WP_TEST_PASSWORD="$wp_app_password" \
  go test -race -count=1 -run '^TestWordPressContainer$' -v ./internal/executioncms
