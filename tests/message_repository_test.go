package tests

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"gochat/internal/model"
	"gochat/internal/repository"

	"gorm.io/gorm"
)

// newTestConversation 创建一条测试用单聊会话，并注册清理逻辑。
func newTestConversation(
	t *testing.T,
	db *gorm.DB,
) *model.Conversation {
	t.Helper()

	repo := repository.NewConversationRepository(db)

	base := uint64(time.Now().UnixNano())
	key := repository.SingleKey(base, base+1)

	conv, err := repo.CreateSingleConversation(context.Background(), base, base+1)
	if err != nil {
		t.Fatalf("create test conversation failed: %v", err)
	}

	t.Cleanup(func() {
		cleanupSingleConversation(t, db, key)
	})

	return conv
}

// TestMessageRepositoryCreateAndPaginate 测试写入消息和 seq 游标分页。
func TestMessageRepositoryCreateAndPaginate(t *testing.T) {
	db := openTestDB(t)

	conv := newTestConversation(t, db)
	repo := repository.NewMessageRepository(db)
	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		msg := &model.Message{
			ConversationID: conv.ID,
			SenderID:       1,
			Seq:            uint64(i),
			ClientMsgID:    fmt.Sprintf("client-msg-%d", i),
			ContentType:    model.MessageContentTypeText,
			Content:        fmt.Sprintf("message %d", i),
		}

		if err := repo.Create(ctx, msg); err != nil {
			t.Fatalf("create message %d failed: %v", i, err)
		}

		if msg.ID == 0 {
			t.Fatalf("expected message %d to have an ID", i)
		}
	}

	// 第一页：最新两条。
	latest, err := repo.ListBeforeSeq(ctx, conv.ID, 0, 2)
	if err != nil {
		t.Fatalf("list latest messages failed: %v", err)
	}

	if len(latest) != 2 {
		t.Fatalf("latest page size = %d, want 2", len(latest))
	}

	if latest[0].Seq != 5 || latest[1].Seq != 4 {
		t.Errorf("latest seqs = %d,%d, want 5,4", latest[0].Seq, latest[1].Seq)
	}

	// 第二页：seq < 4 的最新两条（3、2）。
	older, err := repo.ListBeforeSeq(ctx, conv.ID, 4, 2)
	if err != nil {
		t.Fatalf("list older messages failed: %v", err)
	}

	if len(older) != 2 {
		t.Fatalf("older page size = %d, want 2", len(older))
	}

	if older[0].Seq != 3 || older[1].Seq != 2 {
		t.Errorf("older seqs = %d,%d, want 3,2", older[0].Seq, older[1].Seq)
	}

	// 非法 limit 会被归一化到默认值，不会返回超过上限的数据。
	all, err := repo.ListBeforeSeq(ctx, conv.ID, 0, 1000)
	if err != nil {
		t.Fatalf("list messages failed: %v", err)
	}

	if len(all) != 5 {
		t.Errorf("message count = %d, want 5", len(all))
	}
}

// TestMessageRepositoryDuplicateClientMsgID 测试重发同一 client_msg_id 时幂等。
func TestMessageRepositoryDuplicateClientMsgID(t *testing.T) {
	db := openTestDB(t)

	conv := newTestConversation(t, db)
	repo := repository.NewMessageRepository(db)
	ctx := context.Background()

	first := &model.Message{
		ConversationID: conv.ID,
		SenderID:       1,
		Seq:            1,
		ClientMsgID:    "same-client-id",
		ContentType:    model.MessageContentTypeText,
		Content:        "hello",
	}

	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("first create failed: %v", err)
	}

	// 客户端重发：seq 变了，但 client_msg_id 相同。
	duplicate := &model.Message{
		ConversationID: conv.ID,
		SenderID:       1,
		Seq:            2,
		ClientMsgID:    "same-client-id",
		ContentType:    model.MessageContentTypeText,
		Content:        "hello",
	}

	err := repo.Create(ctx, duplicate)
	if !errors.Is(err, repository.ErrDuplicateMessage) {
		t.Fatalf("error = %v, want %v", err, repository.ErrDuplicateMessage)
	}

	var count int64
	if err := db.Model(&model.Message{}).
		Where("conversation_id = ? AND client_msg_id = ?", conv.ID, "same-client-id").
		Count(&count).Error; err != nil {
		t.Fatalf("count messages failed: %v", err)
	}

	if count != 1 {
		t.Errorf("message count = %d, want 1", count)
	}
}

// TestMessageRepositorySeqConflict 测试同一会话内 seq 冲突会被识别。
func TestMessageRepositorySeqConflict(t *testing.T) {
	db := openTestDB(t)

	conv := newTestConversation(t, db)
	repo := repository.NewMessageRepository(db)
	ctx := context.Background()

	first := &model.Message{
		ConversationID: conv.ID,
		SenderID:       1,
		Seq:            1,
		ClientMsgID:    "client-a",
		ContentType:    model.MessageContentTypeText,
		Content:        "first",
	}

	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("first create failed: %v", err)
	}

	conflict := &model.Message{
		ConversationID: conv.ID,
		SenderID:       1,
		Seq:            1,
		ClientMsgID:    "client-b",
		ContentType:    model.MessageContentTypeText,
		Content:        "conflict",
	}

	err := repo.Create(ctx, conflict)
	if !errors.Is(err, repository.ErrSeqConflict) {
		t.Fatalf("error = %v, want %v", err, repository.ErrSeqConflict)
	}
}
