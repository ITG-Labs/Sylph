ALTER TABLE nodes ADD COLUMN applied_inbounds TEXT[], ADD COLUMN tls_ready TEXT[];
UPDATE nodes SET tls_ready = ARRAY(
    SELECT e.key FROM jsonb_each(tls_status) e
    WHERE e.value->>'not_after' IS NOT NULL AND coalesce(e.value->>'error', '') = '' AND (e.value->>'not_after')::timestamptz > now())
WHERE jsonb_typeof(tls_status) = 'object';
