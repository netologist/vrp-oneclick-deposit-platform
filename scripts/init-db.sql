-- Create per-service databases on a single Postgres instance (demo).
SELECT 'CREATE DATABASE merchant' WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'merchant')\gexec
SELECT 'CREATE DATABASE consent'  WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'consent')\gexec
SELECT 'CREATE DATABASE payment'  WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'payment')\gexec
SELECT 'CREATE DATABASE ledger'   WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'ledger')\gexec
