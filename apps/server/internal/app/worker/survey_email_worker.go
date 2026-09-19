package worker

import (
	"context"
	"sync"
	"time"

	"servify/apps/server/internal/app/bootstrap"
	satisfapp "servify/apps/server/internal/modules/satisfaction/application"

	"github.com/sirupsen/logrus"
)

const (
	// surveyEmailScanInterval 与单轮批量大小：CSAT 邮件非实时敏感，60s/50 足够
	surveyEmailScanInterval = 60 * time.Second
	surveyEmailBatchSize    = 50
)

// SurveyEmailWorker 周期性扫描 queued 的 email 渠道 CSAT 调查并真实投递，
// 补上「ScheduleSurvey 仅置 sent 不发送」的缺口。
type SurveyEmailWorker struct {
	service satisfapp.SurveyEmailProcessor
	logger  *logrus.Logger

	// scanInterval/batchSize 供测试注入短间隔；零值取 surveyEmailScanInterval/batch 常量
	scanInterval time.Duration
	batchSize    int

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewSurveyEmailWorker(service satisfapp.SurveyEmailProcessor, logger *logrus.Logger) bootstrap.Worker {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &SurveyEmailWorker{
		service: service,
		logger:  logger,
	}
}

func (w *SurveyEmailWorker) Name() string { return "survey-email-send" }

func (w *SurveyEmailWorker) Start() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil || w.service == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	w.cancel = cancel
	w.done = done
	interval := w.scanInterval
	if interval <= 0 {
		interval = surveyEmailScanInterval
	}
	batchSize := w.batchSize
	if batchSize <= 0 {
		batchSize = surveyEmailBatchSize
	}
	go func() {
		defer close(done)
		initialDelay := jitter(interval, 0.1)
		if initialDelay > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(initialDelay):
			}
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sent, err := w.service.ProcessPendingSurveyEmails(ctx, batchSize)
				if err != nil {
					// 单轮失败不终止 worker（DB 抖动等），下一轮重试
					if w.logger != nil {
						w.logger.WithError(err).Warn("survey-email worker: scan failed")
					}
					continue
				}
				if sent > 0 && w.logger != nil {
					w.logger.Infof("survey-email worker: delivered %d survey emails", sent)
				}
			}
		}
	}()
	return nil
}

func (w *SurveyEmailWorker) Stop(ctx context.Context) error {
	w.mu.Lock()
	cancel := w.cancel
	done := w.done
	w.cancel = nil
	w.done = nil
	w.mu.Unlock()

	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
