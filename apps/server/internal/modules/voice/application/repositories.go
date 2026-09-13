package application

import "context"

type Repository interface {
	StartCall(ctx context.Context, cmd StartCallCommand) (*CallDTO, error)
	AnswerCall(ctx context.Context, cmd AnswerCallCommand) (*CallDTO, error)
	HoldCall(ctx context.Context, cmd HoldCallCommand) (*CallDTO, error)
	ResumeCall(ctx context.Context, cmd ResumeCallCommand) (*CallDTO, error)
	EndCall(ctx context.Context, cmd EndCallCommand) (*CallDTO, error)
	TransferCall(ctx context.Context, cmd TransferCallCommand) (*CallDTO, error)
	// FindByID 供协调层做状态守卫:hosted provider 会重试 webhook,
	// 重复事件须按当前状态幂等短路而非重复执行。
	FindByID(ctx context.Context, callID string) (*CallDTO, error)
}

type RecordingRepository interface {
	Save(ctx context.Context, recording RecordingDTO) error
	MarkStopped(ctx context.Context, recordingID string) error
	FindByID(ctx context.Context, recordingID string) (*RecordingDTO, error)
	// UpsertCompleted 按录音 ID 落终态:存在则补 status/storage_uri,
	// 不存在则直接建完成态记录(回调先于 start 到达的乱序兜底)。
	UpsertCompleted(ctx context.Context, recording RecordingDTO) error
}

type TranscriptRepository interface {
	Append(ctx context.Context, transcript TranscriptDTO) error
	ListByCallID(ctx context.Context, callID string) ([]TranscriptDTO, error)
	ListAll(ctx context.Context, page, pageSize int) ([]TranscriptDTO, int64, error)
}
