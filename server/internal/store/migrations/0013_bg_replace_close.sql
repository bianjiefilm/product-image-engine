-- 0013_bg_replace_close.sql — HUI-1699 收口:模型/任务费用引用与商品分项检查。
-- 不记录资金金额。没有真实模型凭证时,接口仍返回「真实出图未完成」,本表不把空引用写成出图成功。

ALTER TABLE bg_replace_jobs ADD COLUMN model_ref TEXT NOT NULL DEFAULT '';
ALTER TABLE bg_replace_jobs ADD COLUMN fee_ref TEXT NOT NULL DEFAULT '';
ALTER TABLE bg_replace_jobs ADD COLUMN product_checks_json TEXT NOT NULL DEFAULT '{}';
