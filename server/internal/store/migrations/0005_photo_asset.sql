-- 0005_photo_asset.sql — 产品照片上传(HUI-1697 / FEAT-0198)
-- additive 新表;内容幂等键 = (tenant_id, sha256):同租户同内容只入库一次,
-- 重复提交幂等返回同一资产引用(不重复登记、不重复计费)。
-- 只存版本引用与核验事实快照,文件字节永远在平台 upload 设施(本产品不存文件)。

CREATE TABLE IF NOT EXISTS photo_assets (
    id                TEXT PRIMARY KEY,   -- photo_<hex>
    tenant_id         TEXT NOT NULL,
    sha256            TEXT NOT NULL,      -- 服务端计算的内容指纹(客户端声明不作数)
    size_bytes        INTEGER NOT NULL,
    media_type        TEXT NOT NULL,      -- 服务端嗅探+真解码得出的媒体类型
    width_px          INTEGER NOT NULL,
    height_px         INTEGER NOT NULL,
    purpose           TEXT NOT NULL,      -- 用途声明(服务端枚举把关,如 project_photo)
    platform_asset_id TEXT NOT NULL,      -- 经平台 upload 设施登记的 asset_id 引用
    original_name     TEXT NOT NULL DEFAULT '',
    created_by        TEXT NOT NULL,
    created_at        TEXT NOT NULL,
    UNIQUE (tenant_id, sha256)
);

CREATE INDEX IF NOT EXISTS idx_photo_assets_asset ON photo_assets(tenant_id, platform_asset_id);
