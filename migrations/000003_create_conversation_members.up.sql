CREATE TABLE IF NOT EXISTS conversation_members (
    conversation_id BIGINT UNSIGNED NOT NULL,
    user_id BIGINT UNSIGNED NOT NULL,

    -- 0=普通成员 1=群主
    role TINYINT UNSIGNED NOT NULL DEFAULT 0,

    -- 已读到的最大 seq，未读数 = COUNT(*) WHERE seq > last_read_seq。
    last_read_seq BIGINT UNSIGNED NOT NULL DEFAULT 0,

    joined_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

    -- 复合主键：同会话内用户唯一，同时支持"按会话查成员"。
    PRIMARY KEY (conversation_id, user_id),

    -- 反向查询：某用户的所有会话（会话列表页）。
    KEY idx_members_user (user_id, conversation_id)
) ENGINE=InnoDB
  DEFAULT CHARSET=utf8mb4
  COLLATE=utf8mb4_0900_ai_ci;
