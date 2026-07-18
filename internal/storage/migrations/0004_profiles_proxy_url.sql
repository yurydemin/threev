-- 0004_profiles_proxy_url.sql
-- Adds per-profile proxy support: a single URL field on "profiles". The
-- scheme in the URL itself (http/https/socks5) selects HTTP-CONNECT vs
-- SOCKS5 dialing (see s3client/transport.go's applyProxy). No CHECK
-- constraint touches this table, so a plain ADD COLUMN suffices - no
-- rebuild procedure needed (unlike 0003_transfer_queue_zip_type.sql's
-- CHECK-widening case).
ALTER TABLE profiles ADD COLUMN proxy_url TEXT NOT NULL DEFAULT '';
