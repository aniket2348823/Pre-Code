-- Migration: Add audit_logs table for security event tracking
-- Down
DROP TABLE IF EXISTS audit_logs;
