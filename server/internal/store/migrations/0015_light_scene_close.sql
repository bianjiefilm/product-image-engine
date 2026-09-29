-- 0015_light_scene_close.sql — HUI-1700 收口:模型引用可追溯。
-- 不记录凭证,不记录资金金额。没有模型调用时 model_ref 保持空。
-- 失败或 unknown 不得用这份引用替换用户已经选定的输出版本。

ALTER TABLE light_scene_jobs ADD COLUMN model_ref TEXT NOT NULL DEFAULT '';
