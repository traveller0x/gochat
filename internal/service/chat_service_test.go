package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"gochat/internal/model"
	"gochat/internal/repository"
	"gochat/internal/ws"
)

// fakeChatConvRepository 是会话仓储的假实现。
type fakeChatConvRepository struct {
	conv   *model.Conversation
	err    error
	called int
	//会话列表返回值。
	summaries []repository.ConversationSummary
	// 新增：成员校验结果。
	isMember bool
	// 新增：已读上报返回值。
	markReadSeq     uint64
	markReadChanged bool
	markReadErr     error
}

func (f *fakeChatConvRepository) IsMember(
	ctx context.Context,
	conversationID uint64,
	userID uint64,
) (bool, error) {
	return f.isMember, nil
}

func (f *fakeChatConvRepository) ListByUser(
	ctx context.Context,
	userID uint64,
	limit int,
) ([]repository.ConversationSummary, error) {
	return f.summaries, nil
}

func (f *fakeChatConvRepository) CreateSingleConversation(
	ctx context.Context,
	userA uint64,
	userB uint64,
) (*model.Conversation, error) {
	f.called++

	if f.err != nil {
		return nil, f.err
	}

	return f.conv, nil
}

func (f *fakeChatConvRepository) MarkRead(
	ctx context.Context,
	conversationID uint64,
	userID uint64,
	lastReadSeq uint64,
) (uint64, bool, error) {
	if f.markReadErr != nil {
		return 0, false, f.markReadErr
	}

	return f.markReadSeq, f.markReadChanged, nil
}

// fakeChatMsgRepository 是消息仓储的假实现，记录调用参数。
type fakeChatMsgRepository struct {
	msg        *model.Message
	members    []uint64
	duplicated bool
	err        error

	called             int
	lastSenderID       uint64
	lastConversationID uint64
	lastClientMsgID    string
	lastContent        string
	// 新增：历史/补洞返回的消息。
	messages      []model.Message
	afterMessages []model.Message
}

func (f *fakeChatMsgRepository) ListBeforeSeq(
	ctx context.Context,
	conversationID uint64,
	beforeSeq uint64,
	limit int,
) ([]model.Message, error) {
	return f.messages, nil
}

func (f *fakeChatMsgRepository) ListAfterSeq(
	ctx context.Context,
	conversationID uint64,
	afterSeq uint64,
	limit int,
) ([]model.Message, error) {
	return f.afterMessages, nil
}

func (f *fakeChatMsgRepository) AppendMessage(
	ctx context.Context,
	conversationID uint64,
	senderID uint64,
	clientMsgID string,
	contentType uint8,
	content string,
) (*model.Message, []uint64, bool, error) {
	f.called++
	f.lastConversationID = conversationID
	f.lastSenderID = senderID
	f.lastClientMsgID = clientMsgID
	f.lastContent = content

	if f.err != nil {
		return nil, nil, false, f.err
	}

	return f.msg, f.members, f.duplicated, nil
}

// fakePusher 记录每个用户收到的推送。
type fakePusher struct {
	mu   sync.Mutex
	sent map[uint64][][]byte
}

func newFakePusher() *fakePusher {
	return &fakePusher{
		sent: make(map[uint64][][]byte),
	}
}

func (f *fakePusher) SendToUser(userID uint64, payload []byte) int {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.sent[userID] = append(f.sent[userID], payload)

	return 1
}

func (f *fakePusher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	total := 0
	for _, payloads := range f.sent {
		total += len(payloads)
	}

	return total
}

// newTestChatService 创建使用假依赖的 ChatService。
func newTestChatService(
	msgRepo *fakeChatMsgRepository,
	pusher *fakePusher,
) *ChatService {
	return NewChatService(
		&fakeChatConvRepository{conv: &model.Conversation{ID: 1}},
		msgRepo,
		pusher,
	)
}

func newStoredMessage() *model.Message {
	return &model.Message{
		ID:             100,
		ConversationID: 5,
		SenderID:       1,
		Seq:            7,
		ClientMsgID:    "client-msg-1",
		ContentType:    model.MessageContentTypeText,
		Content:        "hello",
		CreatedAt:      time.Now(),
	}
}

// TestChatServiceSendMessageSuccess 测试发送成功会落库并推送给所有成员。
func TestChatServiceSendMessageSuccess(t *testing.T) {
	msgRepo := &fakeChatMsgRepository{
		msg:     newStoredMessage(),
		members: []uint64{1, 2},
	}
	pusher := newFakePusher()
	svc := newTestChatService(msgRepo, pusher)

	result, err := svc.SendMessage(
		context.Background(), 1, 5, "client-msg-1", model.MessageContentTypeText, "hello",
	)
	if err != nil {
		t.Fatalf("SendMessage() unexpected error: %v", err)
	}

	if result.Duplicated {
		t.Error("Duplicated = true, want false")
	}

	if result.Message == nil || result.Message.Seq != 7 {
		t.Fatalf("unexpected message: %+v", result.Message)
	}

	if got := pusher.count(); got != 2 {
		t.Fatalf("pushed %d payloads, want 2", got)
	}

	// 解析推送帧，确认协议正确。
	payload := pusher.sent[2][0]

	var env ws.Envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		t.Fatalf("unmarshal envelope failed: %v", err)
	}

	if env.Type != ws.TypeMessageNew {
		t.Errorf("type = %q, want %q", env.Type, ws.TypeMessageNew)
	}

	var data ws.MessagePayload
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("unmarshal payload failed: %v", err)
	}

	if data.Seq != 7 || data.Content != "hello" {
		t.Errorf("payload = %+v, want seq=7 content=hello", data)
	}
}

// TestChatServiceSendMessageDuplicate 测试重发不会重复推送。
func TestChatServiceSendMessageDuplicate(t *testing.T) {
	msgRepo := &fakeChatMsgRepository{
		msg:        newStoredMessage(),
		members:    []uint64{1, 2},
		duplicated: true,
	}
	pusher := newFakePusher()
	svc := newTestChatService(msgRepo, pusher)

	result, err := svc.SendMessage(
		context.Background(), 1, 5, "client-msg-1", model.MessageContentTypeText, "hello",
	)
	if err != nil {
		t.Fatalf("SendMessage() unexpected error: %v", err)
	}

	if !result.Duplicated {
		t.Error("Duplicated = false, want true")
	}

	if got := pusher.count(); got != 0 {
		t.Errorf("pushed %d payloads on duplicate, want 0", got)
	}
}

// TestChatServiceSendMessageValidation 测试非法参数不会触达仓储层。
func TestChatServiceSendMessageValidation(t *testing.T) {
	tests := []struct {
		name           string
		senderID       uint64
		conversationID uint64
		clientMsgID    string
		contentType    uint8
		content        string
	}{
		{name: "zero sender", senderID: 0, conversationID: 5, clientMsgID: "c1", contentType: 1, content: "hi"},
		{name: "zero conversation", senderID: 1, conversationID: 0, clientMsgID: "c1", contentType: 1, content: "hi"},
		{name: "empty client msg id", senderID: 1, conversationID: 5, clientMsgID: "", contentType: 1, content: "hi"},
		{name: "too long client msg id", senderID: 1, conversationID: 5, clientMsgID: strings.Repeat("a", 65), contentType: 1, content: "hi"},
		{name: "unsupported content type", senderID: 1, conversationID: 5, clientMsgID: "c1", contentType: 9, content: "hi"},
		{name: "blank content", senderID: 1, conversationID: 5, clientMsgID: "c1", contentType: 1, content: "   "},
		{name: "too long content", senderID: 1, conversationID: 5, clientMsgID: "c1", contentType: 1, content: strings.Repeat("测", MaxMessageContentRunes+1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgRepo := &fakeChatMsgRepository{msg: newStoredMessage()}
			svc := newTestChatService(msgRepo, newFakePusher())

			_, err := svc.SendMessage(
				context.Background(),
				tt.senderID,
				tt.conversationID,
				tt.clientMsgID,
				tt.contentType,
				tt.content,
			)
			if !errors.Is(err, ErrInvalidMessage) {
				t.Errorf("error = %v, want %v", err, ErrInvalidMessage)
			}

			if msgRepo.called != 0 {
				t.Errorf("repository was called %d times, want 0", msgRepo.called)
			}
		})
	}
}

// TestChatServiceSendMessageNotMember 测试非成员无法发送消息。
func TestChatServiceSendMessageNotMember(t *testing.T) {
	msgRepo := &fakeChatMsgRepository{err: repository.ErrNotConversationMember}
	pusher := newFakePusher()
	svc := newTestChatService(msgRepo, pusher)

	_, err := svc.SendMessage(
		context.Background(), 9, 5, "client-msg-1", model.MessageContentTypeText, "hello",
	)
	if !errors.Is(err, ErrNotConversationMember) {
		t.Fatalf("error = %v, want %v", err, ErrNotConversationMember)
	}

	if got := pusher.count(); got != 0 {
		t.Errorf("pushed %d payloads, want 0", got)
	}
}

// TestChatServiceSendMessageConversationNotFound 测试会话不存在时的错误映射。
func TestChatServiceSendMessageConversationNotFound(t *testing.T) {
	msgRepo := &fakeChatMsgRepository{err: repository.ErrConversationNotFound}
	svc := newTestChatService(msgRepo, newFakePusher())

	_, err := svc.SendMessage(
		context.Background(), 1, 404, "client-msg-1", model.MessageContentTypeText, "hello",
	)
	if !errors.Is(err, ErrConversationNotFound) {
		t.Fatalf("error = %v, want %v", err, ErrConversationNotFound)
	}
}

// TestChatServiceCreateSingleConversationInvalid 测试非法用户参数。
func TestChatServiceCreateSingleConversationInvalid(t *testing.T) {
	convRepo := &fakeChatConvRepository{conv: &model.Conversation{ID: 1}}
	svc := NewChatService(convRepo, &fakeChatMsgRepository{}, newFakePusher())

	_, err := svc.CreateSingleConversation(context.Background(), 1, 1)
	if !errors.Is(err, ErrInvalidConversation) {
		t.Fatalf("error = %v, want %v", err, ErrInvalidConversation)
	}

	if convRepo.called != 0 {
		t.Errorf("repository was called %d times, want 0", convRepo.called)
	}
}

// TestChatServiceMarkReadPushesWhenChanged 测试已读前进时广播 message.read。
func TestChatServiceMarkReadPushesWhenChanged(t *testing.T) {
	pusher := newFakePusher()

	svc := NewChatService(
		&fakeChatConvRepository{markReadSeq: 5, markReadChanged: true},
		&fakeChatMsgRepository{},
		pusher,
	)

	result, err := svc.MarkRead(context.Background(), 1, 7, 5)
	if err != nil {
		t.Fatalf("MarkRead() unexpected error: %v", err)
	}

	if result.LastReadSeq != 5 || !result.Changed {
		t.Errorf("result = %+v, want seq 5 changed true", result)
	}

	if got := pusher.count(); got != 1 {
		t.Fatalf("pushed %d payloads, want 1", got)
	}

	var env ws.Envelope
	if err := json.Unmarshal(pusher.sent[1][0], &env); err != nil {
		t.Fatalf("unmarshal envelope failed: %v", err)
	}

	if env.Type != ws.TypeMessageRead {
		t.Errorf("type = %q, want %q", env.Type, ws.TypeMessageRead)
	}
}

// TestChatServiceMarkReadUnchangedDoesNotPush 测试重复上报不重复广播。
func TestChatServiceMarkReadUnchangedDoesNotPush(t *testing.T) {
	pusher := newFakePusher()

	svc := NewChatService(
		&fakeChatConvRepository{markReadSeq: 5, markReadChanged: false},
		&fakeChatMsgRepository{},
		pusher,
	)

	result, err := svc.MarkRead(context.Background(), 1, 7, 5)
	if err != nil {
		t.Fatalf("MarkRead() unexpected error: %v", err)
	}

	if result.Changed {
		t.Error("Changed = true, want false")
	}

	if got := pusher.count(); got != 0 {
		t.Errorf("pushed %d payloads, want 0", got)
	}
}

// TestChatServiceMarkReadInvalid 测试非法参数。
func TestChatServiceMarkReadInvalid(t *testing.T) {
	svc := NewChatService(
		&fakeChatConvRepository{},
		&fakeChatMsgRepository{},
		newFakePusher(),
	)

	_, err := svc.MarkRead(context.Background(), 1, 7, 0)
	if !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("error = %v, want %v", err, ErrInvalidMessage)
	}
}
