CREATE TABLE IF NOT EXISTS conversations (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,

    -- 1=单聊 2=群聊
    type TINYINT UNSIGNED NOT NULL DEFAULT 1,

    -- 单聊唯一键："小用户ID:大用户ID"，保证 A→B 和 B→A 是同一个会话。
    -- 群聊为 NULL：MySQL 唯一索引允许多个 NULL，但不允许多个空字符串。
    single_key VARCHAR(64) NULL DEFAULT NULL,

    name VARCHAR(64) NOT NULL DEFAULT '',
    owner_id BIGINT UNSIGNED NOT NULL DEFAULT 0,

    -- 会话列表页的冗余字段，避免每次聚合查询 messages 表。
    last_message_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
    last_message_at DATETIME(3) NULL DEFAULT NULL,

    -- 会话内消息序号计数器，发消息时用行锁读取并递增。
    last_seq BIGINT UNSIGNED NOT NULL DEFAULT 0,

    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
        ON UPDATE CURRENT_TIMESTAMP(3),

    PRIMARY KEY (id),
    UNIQUE KEY uk_conversations_single_key (single_key)
) ENGINE=InnoDB
  DEFAULT CHARSET=utf8mb4
  COLLATE=utf8mb4_0900_ai_ci;
