package tests

import (
	"context"
	"errors"
	"sync"
	"testing"

	"gochat/internal/model"
	"gochat/internal/repository"
)

// TestAppendMessageSeqIncrements 测试 seq 在会话内连续递增。
func TestAppendMessageSeqIncrements(t *testing.T) {
	db := openTestDB(t)

	conv, userA, _ := newTestConversationWithMembers(t, db)
	repo := repository.NewMessageRepository(db)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		msg, members, duplicated, err := repo.AppendMessage(
			ctx, conv.ID, userA, "client-seq-"+string(rune('a'+i)), model.MessageContentTypeText, "hi",
		)
		if err != nil {
			t.Fatalf("append message %d failed: %v", i, err)
		}

		if duplicated {
			t.Fatalf("message %d marked duplicated, want false", i)
		}

		if msg.Seq != uint64(i) {
			t.Errorf("seq = %d, want %d", msg.Seq, i)
		}

		if len(members) != 2 {
			t.Errorf("members = %d, want 2", len(members))
		}
	}

	// 会话的 last_seq 必须同步更新。
	updated, err := repository.NewConversationRepository(db).GetByID(ctx, conv.ID)
	if err != nil {
		t.Fatalf("get conversation failed: %v", err)
	}

	if updated.LastSeq != 3 {
		t.Errorf("last_seq = %d, want 3", updated.LastSeq)
	}

	if updated.LastMessageID == 0 {
		t.Error("last_message_id should be updated")
	}
}

// TestAppendMessageConcurrent 测试 10 个并发发送得到 1..10 的不重复序号。
func TestAppendMessageConcurrent(t *testing.T) {
	db := openTestDB(t)

	conv, userA, _ := newTestConversationWithMembers(t, db)
	repo := repository.NewMessageRepository(db)
	ctx := context.Background()

	const workers = 10

	seqs := make([]uint64, workers)
	errs := make([]error, workers)

	var wg sync.WaitGroup

	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()

			msg, _, _, err := repo.AppendMessage(
				ctx,
				conv.ID,
				userA,
				"concurrent-"+string(rune('a'+i)),
				model.MessageContentTypeText,
				"hi",
			)
			if err != nil {
				errs[i] = err
				return
			}

			seqs[i] = msg.Seq
		}(i)
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d failed: %v", i, err)
		}
	}

	seen := make(map[uint64]bool, workers)
	for _, seq := range seqs {
		if seen[seq] {
			t.Fatalf("duplicated seq %d", seq)
		}

		seen[seq] = true
	}

	for i := uint64(1); i <= workers; i++ {
		if !seen[i] {
			t.Errorf("missing seq %d", i)
		}
	}

	updated, err := repository.NewConversationRepository(db).GetByID(ctx, conv.ID)
	if err != nil {
		t.Fatalf("get conversation failed: %v", err)
	}

	if updated.LastSeq != workers {
		t.Errorf("last_seq = %d, want %d", updated.LastSeq, workers)
	}
}

// TestAppendMessageDuplicate 测试重发返回已有消息且标记 duplicated。
func TestAppendMessageDuplicate(t *testing.T) {
	db := openTestDB(t)

	conv, userA, _ := newTestConversationWithMembers(t, db)
	repo := repository.NewMessageRepository(db)
	ctx := context.Background()

	first, _, duplicated, err := repo.AppendMessage(
		ctx, conv.ID, userA, "same-client-msg", model.MessageContentTypeText, "hello",
	)
	if err != nil {
		t.Fatalf("first append failed: %v", err)
	}

	if duplicated {
		t.Fatal("first append should not be duplicated")
	}

	second, _, duplicated, err := repo.AppendMessage(
		ctx, conv.ID, userA, "same-client-msg", model.MessageContentTypeText, "hello again",
	)
	if err != nil {
		t.Fatalf("second append failed: %v", err)
	}

	if !duplicated {
		t.Fatal("second append should be duplicated")
	}

	if second.ID != first.ID || second.Seq != first.Seq {
		t.Errorf(
			"second message id/seq = %d/%d, want %d/%d",
			second.ID, second.Seq, first.ID, first.Seq,
		)
	}

	var count int64
	if err := db.Model(&model.Message{}).
		Where("conversation_id = ? AND client_msg_id = ?", conv.ID, "same-client-msg").
		Count(&count).Error; err != nil {
		t.Fatalf("count messages failed: %v", err)
	}

	if count != 1 {
		t.Errorf("message count = %d, want 1", count)
	}
}

// TestAppendMessageNotMember 测试非成员无法写入消息。
func TestAppendMessageNotMember(t *testing.T) {
	db := openTestDB(t)

	conv, _, userB := newTestConversationWithMembers(t, db)
	repo := repository.NewMessageRepository(db)
	ctx := context.Background()

	outsider := userB + 1000

	_, _, _, err := repo.AppendMessage(
		ctx, conv.ID, outsider, "outsider-msg", model.MessageContentTypeText, "hack",
	)
	if !errors.Is(err, repository.ErrNotConversationMember) {
		t.Fatalf("error = %v, want %v", err, repository.ErrNotConversationMember)
	}

	var count int64
	if err := db.Model(&model.Message{}).
		Where("conversation_id = ?", conv.ID).
		Count(&count).Error; err != nil {
		t.Fatalf("count messages failed: %v", err)
	}

	if count != 0 {
		t.Errorf("message count = %d, want 0", count)
	}
}
