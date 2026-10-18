#!/bin/sh
# Runs once, when the Postgres data volume is first initialised.
#
# Creates a least-privilege, read-only role `webapp` that the holybook server
# connects as. The server only ever SELECTs, so it never needs write or DDL
# rights; all writes happen through the importer, which connects as the
# POSTGRES_USER superuser. ALTER DEFAULT PRIVILEGES ensures that the tables the
# importer drops and recreates on every sync automatically grant SELECT to
# webapp, so the read role keeps working across reimports.
set -e

psql -v ON_ERROR_STOP=1 \
  -v owner="$POSTGRES_USER" \
  -v dbname="$POSTGRES_DB" \
  -v webapp_password="$WEBAPP_PASSWORD" \
  --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-'SQL'
	CREATE ROLE webapp LOGIN PASSWORD :'webapp_password';
	GRANT CONNECT ON DATABASE :"dbname" TO webapp;
	GRANT USAGE ON SCHEMA public TO webapp;
	GRANT SELECT ON ALL TABLES IN SCHEMA public TO webapp;
	ALTER DEFAULT PRIVILEGES FOR ROLE :"owner" IN SCHEMA public
	  GRANT SELECT ON TABLES TO webapp;
SQL
