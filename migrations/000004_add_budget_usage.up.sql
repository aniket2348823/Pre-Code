-- Persistent budget usage counters so spend survives process restarts.
-- Keys are namespaced: "org:<uuid>" and "task:<uuid>".
CREATE TABLE IF NOT EXISTS budget_usage (
    key        TEXT PRIMARY KEY,
    amount     DECIMAL(14,6) NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ   NOT NULL DEFAULT now()
);
