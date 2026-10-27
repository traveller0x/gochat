package tests

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"gochat/internal/model"
	"gochat/internal/repository"
)

// TestConversationRepositoryCreateSingleIdempotent 测试单聊创建是幂等的。
func TestConversationRepositoryCreateSingleIdempotent(t *testing.T) {
	db := openTestDB(t)

	repo := repository.NewConversationRepository(db)
	ctx := context.Background()

	// 用时间戳生成不会与已有数据冲突的用户 ID。
	base := uint64(time.Now().UnixNano())
	userA, userB := base, base+1
	key := repository.SingleKey(userA, userB)

	cleanupSingleConversation(t, db, key)
	t.Cleanup(func() {
		cleanupSingleConversation(t, db, key)
	})

	first, err := repo.CreateSingleConversation(ctx, userA, userB)
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}

	if first.ID == 0 {
		t.Fatal("expected conversation ID to be generated")
	}

	if first.Type != model.ConversationTypeSingle {
		t.Errorf("type = %d, want %d", first.Type, model.ConversationTypeSingle)
	}

	// 用户顺序反过来，必须命中同一个会话。
	second, err := repo.CreateSingleConversation(ctx, userB, userA)
	if err != nil {
		t.Fatalf("second create failed: %v", err)
	}

	if second.ID != first.ID {
		t.Fatalf("second conversation ID = %d, want %d", second.ID, first.ID)
	}

	var count int64
	if err := db.Model(&model.Conversation{}).
		Where("single_key = ?", key).
		Count(&count).Error; err != nil {
		t.Fatalf("count conversations failed: %v", err)
	}

	if count != 1 {
		t.Errorf("conversation count = %d, want 1", count)
	}

	ok, err := repo.IsMember(ctx, first.ID, userA)
	if err != nil {
		t.Fatalf("IsMember failed: %v", err)
	}

	if !ok {
		t.Error("userA should be a member")
	}

	ok, err = repo.IsMember(ctx, first.ID, base+100)
	if err != nil {
		t.Fatalf("IsMember failed: %v", err)
	}

	if ok {
		t.Error("outsider should not be a member")
	}
}

// TestConversationRepositoryConcurrentCreateSingle 用 20 个并发请求
// 验证唯一索引能把并发创建收敛成一条会话。
func TestConversationRepositoryConcurrentCreateSingle(t *testing.T) {
	db := openTestDB(t)

	repo := repository.NewConversationRepository(db)
	ctx := context.Background()

	base := uint64(time.Now().UnixNano())
	userA, userB := base, base+1
	key := repository.SingleKey(userA, userB)

	cleanupSingleConversation(t, db, key)
	t.Cleanup(func() {
		cleanupSingleConversation(t, db, key)
	})

	const workers = 20

	ids := make([]uint64, workers)
	errs := make([]error, workers)

	var wg sync.WaitGroup

	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()

			conv, err := repo.CreateSingleConversation(ctx, userA, userB)
			if err != nil {
				errs[i] = err
				return
			}

			ids[i] = conv.ID
		}(i)
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d failed: %v", i, err)
		}
	}

	for i := 1; i < workers; i++ {
		if ids[i] != ids[0] {
			t.Fatalf("worker %d got conversation %d, want %d", i, ids[i], ids[0])
		}
	}

	var convCount int64
	if err := db.Model(&model.Conversation{}).
		Where("single_key = ?", key).
		Count(&convCount).Error; err != nil {
		t.Fatalf("count conversations failed: %v", err)
	}

	if convCount != 1 {
		t.Errorf("conversation count = %d, want 1", convCount)
	}

	var memberCount int64
	if err := db.Model(&model.ConversationMember{}).
		Where("conversation_id = ?", ids[0]).
		Count(&memberCount).Error; err != nil {
		t.Fatalf("count members failed: %v", err)
	}

	if memberCount != 2 {
		t.Errorf("member count = %d, want 2", memberCount)
	}
}

// TestConversationRepositoryListByUser 测试会话列表的未读数、对方信息和排序。
func TestConversationRepositoryListByUser(t *testing.T) {
	db := openTestDB(t)

	userA := newTestUser(t, db, "peera")
	userB := newTestUser(t, db, "peerb")

	conv := newTestConversationBetween(t, db, userA, userB)

	convRepo := repository.NewConversationRepository(db)
	msgRepo := repository.NewMessageRepository(db)
	ctx := context.Background()

	// A 发两条消息。
	for i := 1; i <= 2; i++ {
		if _, _, _, err := msgRepo.AppendMessage(
			ctx,
			conv.ID,
			userA.ID,
			fmt.Sprintf("list-client-%d", i),
			model.MessageContentTypeText,
			fmt.Sprintf("message %d", i),
		); err != nil {
			t.Fatalf("append message %d failed: %v", i, err)
		}
	}

	// B 看到 2 条未读、对方是 A、最后一条是 seq 2。
	summariesB, err := convRepo.ListByUser(ctx, userB.ID, 10)
	if err != nil {
		t.Fatalf("ListByUser(userB) failed: %v", err)
	}

	if len(summariesB) != 1 {
		t.Fatalf("userB conversations = %d, want 1", len(summariesB))
	}

	if summariesB[0].UnreadCount != 2 {
		t.Errorf("userB unread = %d, want 2", summariesB[0].UnreadCount)
	}

	if summariesB[0].Peer == nil || summariesB[0].Peer.ID != userA.ID {
		t.Errorf("userB peer = %+v, want user %d", summariesB[0].Peer, userA.ID)
	}

	if summariesB[0].Peer != nil && summariesB[0].Peer.Nickname != userA.Nickname {
		t.Errorf("peer nickname = %q, want %q", summariesB[0].Peer.Nickname, userA.Nickname)
	}

	if summariesB[0].LastMessage == nil || summariesB[0].LastMessage.Seq != 2 {
		t.Errorf("userB last message = %+v, want seq 2", summariesB[0].LastMessage)
	}

	// A 自己发的消息不算未读，且对方是 B。
	summariesA, err := convRepo.ListByUser(ctx, userA.ID, 10)
	if err != nil {
		t.Fatalf("ListByUser(userA) failed: %v", err)
	}

	if len(summariesA) != 1 {
		t.Fatalf("userA conversations = %d, want 1", len(summariesA))
	}

	if summariesA[0].UnreadCount != 0 {
		t.Errorf("userA unread = %d, want 0（自己发的不算未读）", summariesA[0].UnreadCount)
	}

	if summariesA[0].Peer == nil || summariesA[0].Peer.ID != userB.ID {
		t.Errorf("userA peer = %+v, want user %d", summariesA[0].Peer, userB.ID)
	}
}
