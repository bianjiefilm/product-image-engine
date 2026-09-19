-- 0003_size_variant.sql — 电商尺寸适配(HUI-1703 / FEAT-0204)
-- additive:project_outputs 加列承载 kind=size_variant 变体;变体唯一键 = 拍板三元
-- (source_output_id, preset_name, variant_mode),部分索引限定 kind='size_variant'
-- (result 行三列皆空值,不参与唯一键)。纯确定性变换,不触生成/计费。

-- 输出类别:result(用户登记的成果,默认)| size_variant(尺寸适配变体)
ALTER TABLE project_outputs ADD COLUMN kind TEXT NOT NULL DEFAULT 'result';
-- 变体溯源三元组 + 输出尺寸(仅 kind='size_variant' 语义化)
ALTER TABLE project_outputs ADD COLUMN source_output_id TEXT NOT NULL DEFAULT '';
ALTER TABLE project_outputs ADD COLUMN preset_name TEXT NOT NULL DEFAULT '';
ALTER TABLE project_outputs ADD COLUMN variant_mode TEXT NOT NULL DEFAULT '';
ALTER TABLE project_outputs ADD COLUMN variant_width INTEGER NOT NULL DEFAULT 0;
ALTER TABLE project_outputs ADD COLUMN variant_height INTEGER NOT NULL DEFAULT 0;

CREATE UNIQUE INDEX IF NOT EXISTS idx_outputs_size_variant_key
    ON project_outputs(source_output_id, preset_name, variant_mode)
    WHERE kind = 'size_variant';
CREATE INDEX IF NOT EXISTS idx_outputs_kind ON project_outputs(kind);

-- 变体字节本地留档(支撑零依赖下载;仅本功能自产衍生件,源素材仍零字节)。
CREATE TABLE IF NOT EXISTS size_variant_blobs (
    variant_id TEXT PRIMARY KEY REFERENCES project_outputs(id),
    content    BLOB NOT NULL
);
