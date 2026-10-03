CREATE TABLE IF NOT EXISTS schema_version (
  version INT NOT NULL PRIMARY KEY
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS games (
  id CHAR(36) NOT NULL PRIMARY KEY,
  revision BIGINT UNSIGNED NOT NULL,
  status VARCHAR(16) NOT NULL,
  active_slot TINYINT NULL,
  record LONGBLOB NOT NULL,
  UNIQUE KEY one_active_game (active_slot)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS actions (
  game_id CHAR(36) NOT NULL,
  sequence BIGINT UNSIGNED NOT NULL,
  hand_no INT UNSIGNED NOT NULL,
  record BLOB NOT NULL,
  PRIMARY KEY (game_id, sequence),
  CONSTRAINT actions_game FOREIGN KEY (game_id) REFERENCES games(id)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS hands (
  game_id CHAR(36) NOT NULL,
  hand_no INT UNSIGNED NOT NULL,
  record LONGBLOB NOT NULL,
  PRIMARY KEY (game_id, hand_no),
  CONSTRAINT hands_game FOREIGN KEY (game_id) REFERENCES games(id)
) ENGINE=InnoDB;

INSERT IGNORE INTO schema_version (version) VALUES (1);
