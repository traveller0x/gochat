package tests

import (
	"context"
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
