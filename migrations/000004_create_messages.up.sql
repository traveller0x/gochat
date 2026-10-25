CREATE TABLE IF NOT EXISTS messages (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    conversation_id BIGINT UNSIGNED NOT NULL,
    sender_id BIGINT UNSIGNED NOT NULL,

    -- 会话内连续递增序号，用于排序与补洞。
    seq BIGINT UNSIGNED NOT NULL,

    -- 客户端生成的消息 ID，用于重发幂等。
    client_msg_id VARCHAR(64) NOT NULL,

    -- 1=文本
    content_type TINYINT UNSIGNED NOT NULL DEFAULT 1,
    content TEXT NOT NULL,

    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

    PRIMARY KEY (id),

    -- 会话内序号唯一：分页索引 + 并发写入兜底约束。
    UNIQUE KEY uk_messages_conversation_seq (conversation_id, seq),

    -- 同一会话内同一 client_msg_id 只能有一条：重发幂等。
    UNIQUE KEY uk_messages_conversation_client (conversation_id, client_msg_id)
) ENGINE=InnoDB
  DEFAULT CHARSET=utf8mb4
  COLLATE=utf8mb4_0900_ai_ci;
