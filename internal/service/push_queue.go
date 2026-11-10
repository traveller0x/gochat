package service

import (
	"log/slog"

	"gochat/internal/metrics"
)

const (
	// pushQueueSize 推送队列长度。满了就丢弃：
	// 消息已经在库里，客户端重连后按 after_seq 能补回来。
	pushQueueSize = 4096

	// pushWorkers 并发推送协程数。
	pushWorkers = 8
)

type pushJob struct {
	userID  uint64
	payload []byte
}

// startPushWorkers 惰性启动推送协程：第一次入队时才启动。
func (s *ChatService) startPushWorkers() {
	s.pushOnce.Do(func() {
		for i := 0; i < pushWorkers; i++ {
			go func() {
				for job := range s.pushQueue {
					s.pusher.SendToUser(job.userID, job.payload)
				}
			}()
		}
	})
}

// enqueuePush 把推送放进队列，绝不阻塞业务路径。
func (s *ChatService) enqueuePush(userID uint64, payload []byte) {
	s.startPushWorkers()

	select {
	case s.pushQueue <- pushJob{userID: userID, payload: payload}:
	default:
		metrics.PushDroppedTotal.Inc()
		slog.Default().Warn("push queue full, dropping push", "user_id", userID)
	}
}
