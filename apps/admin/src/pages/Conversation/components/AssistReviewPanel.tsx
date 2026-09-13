import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Button, Empty, Space, Tag, Tooltip, message } from 'antd';
import {
  ArrowRightOutlined,
  BorderOutlined,
  DeleteOutlined,
  EditOutlined,
  UndoOutlined,
} from '@ant-design/icons';
import {
  addAnnotation,
  deleteAnnotation,
  getAssistSession,
  listAnnotations,
} from '@/services/remoteAssist';

type ShapeTool = 'rect' | 'freehand' | 'arrow';

const SHAPE_TOOLS: Array<{ key: ShapeTool; label: string; icon: React.ReactNode }> = [
  { key: 'rect', label: '矩形', icon: <BorderOutlined /> },
  { key: 'freehand', label: '画笔', icon: <EditOutlined /> },
  { key: 'arrow', label: '箭头', icon: <ArrowRightOutlined /> },
];

const STROKE_COLOR = '#ff4d4f';

interface NormalizedPoint {
  x: number;
  y: number;
}

interface AnnotationShape {
  x?: number;
  y?: number;
  w?: number;
  h?: number;
  x1?: number;
  y1?: number;
  x2?: number;
  y2?: number;
  points?: NormalizedPoint[];
}

interface CanvasAnnotation {
  id: number;
  timestamp_ms: number;
  shape: string;
  payload: AnnotationShape;
}

/** 服务端 payload 是 JSON 文本，解析失败按丢弃处理 */
function parseAnnotation(raw: API.RemoteAssistAnnotation): CanvasAnnotation | null {
  try {
    const payload = JSON.parse(raw.payload) as AnnotationShape;
    if (!payload || typeof payload !== 'object') {
      return null;
    }
    return { id: raw.id, timestamp_ms: raw.timestamp_ms, shape: raw.shape, payload };
  } catch {
    return null;
  }
}

function formatTimestamp(ms: number): string {
  const totalSeconds = ms / 1000;
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds - minutes * 60;
  return `${String(minutes).padStart(2, '0')}:${seconds.toFixed(1).padStart(4, '0')}`;
}

function drawShape(
  ctx: CanvasRenderingContext2D,
  shape: string,
  payload: AnnotationShape,
  width: number,
  height: number,
): void {
  ctx.strokeStyle = STROKE_COLOR;
  ctx.fillStyle = STROKE_COLOR;
  ctx.lineWidth = 2;
  ctx.lineCap = 'round';
  ctx.lineJoin = 'round';

  if (shape === 'rect') {
    const { x, y, w, h } = payload;
    if ([x, y, w, h].some((v) => typeof v !== 'number')) return;
    ctx.strokeRect(x! * width, y! * height, w! * width, h! * height);
    return;
  }

  if (shape === 'freehand') {
    const points = payload.points || [];
    if (points.length < 2) return;
    ctx.beginPath();
    ctx.moveTo(points[0].x * width, points[0].y * height);
    for (const point of points.slice(1)) {
      ctx.lineTo(point.x * width, point.y * height);
    }
    ctx.stroke();
    return;
  }

  if (shape === 'arrow') {
    const { x1, y1, x2, y2 } = payload;
    if ([x1, y1, x2, y2].some((v) => typeof v !== 'number')) return;
    const from = { x: x1! * width, y: y1! * height };
    const to = { x: x2! * width, y: y2! * height };
    ctx.beginPath();
    ctx.moveTo(from.x, from.y);
    ctx.lineTo(to.x, to.y);
    ctx.stroke();

    // 箭头头部：两条短边
    const angle = Math.atan2(to.y - from.y, to.x - from.x);
    const headLength = Math.max(10, Math.min(24, Math.hypot(to.x - from.x, to.y - from.y) * 0.25));
    ctx.beginPath();
    ctx.moveTo(to.x, to.y);
    ctx.lineTo(
      to.x - headLength * Math.cos(angle - Math.PI / 7),
      to.y - headLength * Math.sin(angle - Math.PI / 7),
    );
    ctx.moveTo(to.x, to.y);
    ctx.lineTo(
      to.x - headLength * Math.cos(angle + Math.PI / 7),
      to.y - headLength * Math.sin(angle + Math.PI / 7),
    );
    ctx.stroke();
  }
}

interface AssistReviewPanelProps {
  session: API.RemoteAssistSession;
}

/** 协助结束后：录制回放 + Canvas 覆盖层标注（标注按归一化坐标存 API，点击列表 seek） */
const AssistReviewPanel: React.FC<AssistReviewPanelProps> = ({ session }) => {
  // 访客录制「上传→回写」有延迟：recording_key 未就绪时轮询重取（最多 3 次）
  const [detail, setDetail] = useState<API.RemoteAssistSession>(session);
  const [retryCount, setRetryCount] = useState(0);
  const videoRef = useRef<HTMLVideoElement | null>(null);
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const wrapRef = useRef<HTMLDivElement | null>(null);
  const drawingRef = useRef<{ tool: ShapeTool; points: NormalizedPoint[]; start: NormalizedPoint } | null>(null);
  const [annotations, setAnnotations] = useState<CanvasAnnotation[]>([]);
  const [tool, setTool] = useState<ShapeTool | null>(null);
  const [saving, setSaving] = useState(false);
  const [currentTimeMs, setCurrentTimeMs] = useState(0);

  const redraw = useCallback(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    for (const ann of annotations) {
      drawShape(ctx, ann.shape, ann.payload, canvas.width, canvas.height);
    }

    // 绘制中的预览
    const drawing = drawingRef.current;
    if (drawing) {
      if (drawing.tool === 'rect') {
        const [start, end] = [drawing.start, drawing.points[drawing.points.length - 1] || drawing.start];
        drawShape(ctx, 'rect', {
          x: Math.min(start.x, end.x),
          y: Math.min(start.y, end.y),
          w: Math.abs(end.x - start.x),
          h: Math.abs(end.y - start.y),
        }, canvas.width, canvas.height);
      } else if (drawing.tool === 'freehand' && drawing.points.length >= 2) {
        drawShape(ctx, 'freehand', { points: drawing.points }, canvas.width, canvas.height);
      } else if (drawing.tool === 'arrow') {
        const end = drawing.points[drawing.points.length - 1] || drawing.start;
        drawShape(ctx, 'arrow', {
          x1: drawing.start.x, y1: drawing.start.y, x2: end.x, y2: end.y,
        }, canvas.width, canvas.height);
      }
    }
  }, [annotations]);

  // 画布尺寸跟随视频渲染区域
  const syncCanvasSize = useCallback(() => {
    const canvas = canvasRef.current;
    const wrap = wrapRef.current;
    if (!canvas || !wrap) return;
    const { width, height } = wrap.getBoundingClientRect();
    if (width > 0 && height > 0) {
      canvas.width = Math.round(width);
      canvas.height = Math.round(height);
      redraw();
    }
  }, [redraw]);

  const loadAnnotations = useCallback(async () => {
    try {
      const list = await listAnnotations(detail.id);
      setAnnotations(
        (list || [])
          .map(parseAnnotation)
          .filter((item): item is CanvasAnnotation => item !== null),
      );
    } catch (error) {
      console.error('加载协助标注失败:', error);
    }
  }, [detail.id]);

  useEffect(() => {
    loadAnnotations();
  }, [loadAnnotations]);

  useEffect(() => {
    if (detail.recording_key || retryCount >= 3) {
      return;
    }
    const timer = setTimeout(async () => {
      try {
        const latest = await getAssistSession(detail.id);
        if (latest?.recording_key) {
          setDetail(latest);
        }
      } catch {
        // 下一次重试再取
      }
      setRetryCount((count) => count + 1);
    }, 3000);
    return () => clearTimeout(timer);
  }, [detail, retryCount]);

  useEffect(() => {
    syncCanvasSize();
    window.addEventListener('resize', syncCanvasSize);
    return () => window.removeEventListener('resize', syncCanvasSize);
  }, [syncCanvasSize]);

  useEffect(() => {
    redraw();
  }, [redraw]);

  const toNormalized = useCallback((event: React.PointerEvent<HTMLCanvasElement>): NormalizedPoint => {
    const canvas = canvasRef.current!;
    const rect = canvas.getBoundingClientRect();
    return {
      x: Math.min(1, Math.max(0, (event.clientX - rect.left) / rect.width)),
      y: Math.min(1, Math.max(0, (event.clientY - rect.top) / rect.height)),
    };
  }, []);

  const handlePointerDown = useCallback((event: React.PointerEvent<HTMLCanvasElement>) => {
    if (!tool) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    const point = toNormalized(event);
    drawingRef.current = { tool, points: [point], start: point };
    redraw();
  }, [tool, toNormalized, redraw]);

  const handlePointerMove = useCallback((event: React.PointerEvent<HTMLCanvasElement>) => {
    const drawing = drawingRef.current;
    if (!drawing) return;
    const point = toNormalized(event);
    if (drawing.tool === 'freehand') {
      const last = drawing.points[drawing.points.length - 1];
      // 最小位移过滤，避免点列爆炸
      if (!last || Math.hypot(point.x - last.x, point.y - last.y) > 0.002) {
        drawing.points.push(point);
      }
    } else if (drawing.points.length === 0) {
      drawing.points.push(point);
    } else {
      drawing.points[0] = point;
    }
    redraw();
  }, [toNormalized, redraw]);

  const handlePointerUp = useCallback(async () => {
    const drawing = drawingRef.current;
    drawingRef.current = null;
    if (!drawing) return;

    const video = videoRef.current;
    let payload: AnnotationShape | null = null;
    if (drawing.tool === 'rect') {
      const end = drawing.points[drawing.points.length - 1] || drawing.start;
      payload = {
        x: Math.min(drawing.start.x, end.x),
        y: Math.min(drawing.start.y, end.y),
        w: Math.abs(end.x - drawing.start.x),
        h: Math.abs(end.y - drawing.start.y),
      };
    } else if (drawing.tool === 'freehand' && drawing.points.length >= 2) {
      payload = { points: drawing.points };
    } else if (drawing.tool === 'arrow') {
      const end = drawing.points[drawing.points.length - 1] || drawing.start;
      payload = { x1: drawing.start.x, y1: drawing.start.y, x2: end.x, y2: end.y };
    }

    redraw();
    if (!payload || !video) return;

    setSaving(true);
    try {
      const saved = await addAnnotation(detail.id, {
        timestamp_ms: Math.round(video.currentTime * 1000),
        shape: drawing.tool,
        payload: payload as unknown as Record<string, unknown>,
      });
      const parsed = parseAnnotation(saved);
      if (parsed) {
        setAnnotations((prev) => [...prev, parsed]);
      }
    } catch (error) {
      message.error('保存标注失败: ' + (error as Error).message);
    } finally {
      setSaving(false);
    }
  }, [detail.id, redraw]);

  const seekTo = useCallback((ms: number) => {
    const video = videoRef.current;
    if (!video) return;
    video.currentTime = ms / 1000;
  }, []);

  const removeAnnotation = useCallback(async (id: number) => {
    try {
      await deleteAnnotation(id);
      setAnnotations((prev) => prev.filter((item) => item.id !== id));
    } catch (error) {
      message.error('删除标注失败: ' + (error as Error).message);
    }
  }, []);

  const undoLast = useCallback(async () => {
    if (annotations.length === 0) return;
    const last = annotations[annotations.length - 1];
    await removeAnnotation(last.id);
  }, [annotations, removeAnnotation]);

  const sortedAnnotations = useMemo(
    () => [...annotations].sort((a, b) => a.timestamp_ms - b.timestamp_ms),
    [annotations],
  );

  if (!detail.recording_key) {
    return (
      <div style={{ marginTop: 8 }}>
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          description="该协助没有录制回放（访客端未开启录制）。"
          style={{ padding: 12 }}
        />
      </div>
    );
  }

  return (
    <div style={{ marginTop: 8 }}>
      <div
        ref={wrapRef}
        style={{
          position: 'relative',
          borderRadius: 8,
          overflow: 'hidden',
          background: '#000',
          minHeight: 180,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
        }}
      >
        <video
          ref={videoRef}
          src={detail.recording_key}
          controls
          playsInline
          style={{ width: '100%', maxHeight: 280, display: 'block' }}
          onLoadedMetadata={syncCanvasSize}
          onTimeUpdate={(e) => setCurrentTimeMs(e.currentTarget.currentTime * 1000)}
        />
        <canvas
          ref={canvasRef}
          onPointerDown={handlePointerDown}
          onPointerMove={handlePointerMove}
          onPointerUp={handlePointerUp}
          onPointerLeave={handlePointerUp}
          style={{
            position: 'absolute',
            inset: 0,
            width: '100%',
            height: '100%',
            // 非标注模式放行点击，保证视频控制条可用
            pointerEvents: tool ? 'auto' : 'none',
            cursor: 'crosshair',
            touchAction: 'none',
          }}
        />
      </div>

      <div style={{ marginTop: 8, display: 'flex', justifyContent: 'space-between', flexWrap: 'wrap', gap: 8 }}>
        <Space size={[8, 8]} wrap>
          <span style={{ color: '#666' }}>标注工具</span>
          {SHAPE_TOOLS.map((item) => (
            <Tag
              key={item.key}
              color={tool === item.key ? 'red' : 'default'}
              style={{ cursor: 'pointer', userSelect: 'none' }}
              onClick={() => setTool(tool === item.key ? null : item.key)}
            >
              {item.icon} {item.label}
            </Tag>
          ))}
          <Button size="small" icon={<UndoOutlined />} disabled={annotations.length === 0 || saving} onClick={undoLast}>
            撤销
          </Button>
        </Space>
        <span style={{ color: '#999', fontSize: 12 }}>
          播放位置 {formatTimestamp(Math.round(currentTimeMs))}，新标注会记录该时间点
        </span>
      </div>

      {sortedAnnotations.length > 0 && (
        <div style={{ marginTop: 8 }}>
          <div style={{ color: '#666', marginBottom: 4 }}>标注列表</div>
          <Space size={[8, 8]} wrap>
            {sortedAnnotations.map((ann) => {
              const isCurrent = Math.abs(ann.timestamp_ms - currentTimeMs) <= 1000;
              return (
                <Tag
                  key={ann.id}
                  color={isCurrent ? 'red' : 'default'}
                  style={{ cursor: 'pointer', userSelect: 'none' }}
                  onClick={() => seekTo(ann.timestamp_ms)}
                >
                  <Space size={4}>
                    <span onClick={(e) => { e.stopPropagation(); seekTo(ann.timestamp_ms); }}>
                      {formatTimestamp(ann.timestamp_ms)} · {ann.shape}
                    </span>
                    <Tooltip title="删除标注">
                      <DeleteOutlined
                        onClick={(e) => { e.stopPropagation(); void removeAnnotation(ann.id); }}
                      />
                    </Tooltip>
                  </Space>
                </Tag>
              );
            })}
          </Space>
        </div>
      )}
    </div>
  );
};

export default AssistReviewPanel;
