-- 002: 牌桌配置模板 + user 字段预留 + 牌局来源字段。
-- 本文件以分号逐条执行，必须可重复运行：CREATE 用 IF NOT EXISTS，
-- ALTER ADD COLUMN 用 information_schema 守卫 + 预处理语句，避免部分失败后重跑报错。
CREATE TABLE IF NOT EXISTS table_templates (
  id CHAR(36) NOT NULL PRIMARY KEY,
  owner_user_id CHAR(36) NULL,
  name VARCHAR(128) NOT NULL,
  revision BIGINT UNSIGNED NOT NULL,
  config JSON NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  KEY templates_updated (updated_at, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

SET @c_owner := (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='games' AND COLUMN_NAME='owner_user_id');
SET @s_owner := IF(@c_owner=0, 'ALTER TABLE games ADD COLUMN owner_user_id CHAR(36) NULL', 'DO 0');
PREPARE m_owner FROM @s_owner;
EXECUTE m_owner;
DEALLOCATE PREPARE m_owner;

SET @c_tpl := (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='games' AND COLUMN_NAME='template_id');
SET @s_tpl := IF(@c_tpl=0, 'ALTER TABLE games ADD COLUMN template_id CHAR(36) NULL', 'DO 0');
PREPARE m_tpl FROM @s_tpl;
EXECUTE m_tpl;
DEALLOCATE PREPARE m_tpl;

SET @c_tplrev := (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='games' AND COLUMN_NAME='template_revision');
SET @s_tplrev := IF(@c_tplrev=0, 'ALTER TABLE games ADD COLUMN template_revision BIGINT UNSIGNED NULL', 'DO 0');
PREPARE m_tplrev FROM @s_tplrev;
EXECUTE m_tplrev;
DEALLOCATE PREPARE m_tplrev;

INSERT IGNORE INTO schema_version (version) VALUES (2);
