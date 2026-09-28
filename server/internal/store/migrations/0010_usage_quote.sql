-- HUI-1991 产品图用量报价确认。
-- 只保存付款主体引用、用量事实和报价引用，不保存余额、充值或钱包。

CREATE TABLE IF NOT EXISTS image_usage_quotes (
    id                TEXT PRIMARY KEY,
    storage_tenant    TEXT NOT NULL,
    project_id        TEXT NOT NULL,
    payer_kind        TEXT NOT NULL,
    payer_account_ref TEXT NOT NULL,
    payer_display     TEXT NOT NULL,
    image_count       INTEGER NOT NULL,
    resolution        TEXT NOT NULL,
    capability        TEXT NOT NULL,
    pricing_version   TEXT NOT NULL,
    fingerprint       TEXT NOT NULL,
    quote_ref         TEXT NOT NULL DEFAULT '',
    presentation      TEXT NOT NULL DEFAULT '待确认',
    idempotency_key   TEXT NOT NULL,
    task_id           TEXT NOT NULL DEFAULT '',
    charge_ref        TEXT NOT NULL DEFAULT '',
    confirmed         INTEGER NOT NULL DEFAULT 0,
    status            TEXT NOT NULL,
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL,
    UNIQUE (storage_tenant, project_id)
);
