declare namespace API {
  /** 当前用户 */
  interface CurrentUser {
    id?: number;
    username?: string;
    email?: string;
    role?: string;
    principal_kind?: 'admin' | 'agent' | 'service' | 'end_user';
    permissions?: string[];
  }

  /** 通用分页响应 */
  interface PaginatedResponse<T> {
    data: T[];
    total: number;
    page: number;
    page_size: number;
    pages: number;
  }

  /** 通用数据响应 */
  interface DataResponse<T> {
    data: T;
  }

  /** 通用列表响应 */
  interface ListResponse<T> {
    data: T[];
  }

  /** 通用计数列表响应 */
  interface CountedListResponse<T> {
    count: number;
    items: T[];
  }

  /** 错误响应 */
  interface ErrorResponse {
    error: string;
    message: string;
    code?: string;
  }

  /** 成功响应 */
  interface SuccessResponse<T = unknown> {
    message: string;
    data?: T;
  }

  interface MessageResponse<T = unknown> {
    message: string;
    data?: T;
  }

  interface DashboardTrendPoint {
    date: string;
    tickets?: number;
    sessions?: number;
    messages?: number;
    value?: number;
    type?: string;
  }

  interface SatisfactionTrendPoint {
    date: string;
    average_rating: number;
    count: number;
  }

  // ---- 工单 ----
  interface Ticket {
    id: number;
    title: string;
    description?: string;
    status: string;
    priority: string;
    category?: string;
    source?: string;
    session_id?: string;
    customer_id: number;
    customer_name?: string;
    agent_id?: number;
    agent_name?: string;
    created_at: string;
    updated_at: string;
    resolved_at?: string;
    closed_at?: string;
    custom_fields?: Record<string, any>;
    tags?: string[] | string;
    tag_list?: string[];
    satisfaction_score?: number;
  }

  interface TicketListParams {
    page?: number;
    page_size?: number;
    status?: string;
    priority?: string;
    category?: string;
    source?: string;
    tag?: string;
    agent_id?: number;
    customer_id?: number;
    search?: string;
  }

  // ---- 客户 ----
  interface Customer {
    id: number;
    name: string;
    email?: string;
    phone?: string;
    company?: string;
    industry?: string;
    source?: string;
    tags?: string[];
    priority?: string;
    created_at: string;
    updated_at: string;
  }

  // ---- 客服 ----
  interface Agent {
    id: number;
    name: string;
    email: string;
    status: string;
    skills?: string[];
    current_sessions?: number;
    max_sessions?: number;
    created_at: string;
  }

  // ---- 会话 ----
  interface Conversation {
    id: string;
    customer_id: number;
    customer_name?: string;
    agent_id?: number;
    agent_name?: string;
    status: string;
    channel?: string;
    created_at: string;
    updated_at: string;
  }

  interface ConversationMessage {
    id: string;
    conversation_id: string;
    sender: string;
    kind: string;
    content: string;
    metadata?: Record<string, string>;
    created_at: string;
  }

  // ---- 知识库文档 ----
  interface KnowledgeDoc {
    id: number;
    title: string;
    content?: string;
    category?: string;
    tags?: string | string[];
    is_public?: boolean;
    created_at: string;
    updated_at: string;
  }

  // ---- 自动化规则 ----
  interface Automation {
    id: number;
    name: string;
    description?: string;
    event?: string;
    trigger_type?: string;
    conditions?: string | Record<string, any>;
    actions?: string | Record<string, any>;
    active?: boolean;
    enabled?: boolean;
    created_at: string;
    updated_at?: string;
  }

  // ---- 宏 ----
  interface Macro {
    id: number;
    name: string;
    description?: string;
    language?: string;
    content: string;
    category?: string;
    active?: boolean;
    created_at: string;
    updated_at?: string;
  }

  // ---- SLA 配置 ----
  interface SLAConfig {
    id: number;
    name: string;
    description?: string;
    priority: string;
    customer_tier?: string;
    tags?: string | string[];
    warning_threshold?: number;
    first_response_time: number;
    resolution_time: number;
    escalation_time?: number;
    business_hours_only?: boolean;
    active?: boolean;
    enabled?: boolean;
    created_at?: string;
    updated_at?: string;
  }

  // ---- 班次 ----
  interface Shift {
    id: number;
    agent_id: number;
    agent?: {
      id?: number;
      name?: string;
      username?: string;
    };
    agent_name?: string;
    shift_type?: string;
    start_time: string;
    end_time: string;
    date?: string;
    status: string;
    created_at?: string;
    updated_at?: string;
  }

  // ---- 满意度 ----
  interface Satisfaction {
    id: number;
    ticket_id: number;
    customer_id: number;
    score: number;
    comment?: string;
    created_at: string;
  }

  interface SatisfactionStats {
    average_rating: number;
    total_ratings: number;
    rating_distribution: Record<string, number>;
    trend_data: Array<{
      date: string;
      average_rating: number;
      count: number;
    }>;
    category_stats?: Record<string, {
      average_rating: number;
      count?: number;
    }>;
  }

  interface SatisfactionSurvey {
    id: number;
    ticket_id: number;
    customer_id?: number;
    agent_id?: number;
    channel?: string;
    status?: string;
    survey_token?: string;
    sent_at?: string;
    expires_at?: string;
    completed_at?: string;
    satisfaction_id?: number;
    created_at: string;
    updated_at?: string;
  }

  // ---- 自定义字段 ----
  interface CustomField {
    id: number;
    name: string;
    key: string;
    type?: 'string' | 'number' | 'boolean' | 'date' | 'select' | 'multi_select' | 'multiselect';
    field_type?: 'string' | 'number' | 'boolean' | 'date' | 'select' | 'multi_select' | 'multiselect';
    options?: string[];
    options_json?: string;
    required: boolean;
    active?: boolean;
    resource?: string;
    entity_type?: string;
    created_at?: string;
    updated_at?: string;
  }

  // ---- 审计日志 ----
  interface AuditLog {
    id: number;
    action: string;
    resource_type: string;
    resource_id: string;
    actor_user_id?: number;
    principal_kind?: string;
    route?: string;
    method?: string;
    status_code?: number;
    request_id?: string;
    client_ip?: string;
    user_agent?: string;
    request_json?: string;
    before_json?: string;
    after_json?: string;
    success: boolean;
    created_at: string;
  }

  /** 质检记录（规则违规 + LLM 打分 + 人工复核） */
  interface QualityReview {
    id: number;
    tenant_id: string;
    workspace_id: string;
    session_id: string;
    customer_id?: number;
    agent_id?: number;
    status: 'pending' | 'skipped' | 'scored' | 'failed' | 'confirmed';
    trigger: 'worker' | 'manual' | 'rescore';
    message_count: number;
    duration_seconds: number;
    violations_json: string;
    violation_count: number;
    max_severity: '' | 'low' | 'medium' | 'high';
    dimensions_json: string;
    llm_total_score?: number;
    llm_summary: string;
    llm_provider: string;
    llm_model: string;
    attempt_count: number;
    next_retry_at?: string;
    last_error: string;
    manual_score?: number;
    manual_result: '' | 'pass' | 'violation';
    review_note: string;
    reviewed_by?: number;
    reviewed_at?: string;
    scored_at?: string;
    created_at: string;
    updated_at: string;
  }

  /** 质检记录列表响应（{items,total} 风格） */
  interface QualityReviewListResponse {
    items: QualityReview[];
    total: number;
    page: number;
    page_size: number;
  }

  /** 质检违规明细（violations_json 数组元素） */
  interface QualityViolation {
    rule: string;
    severity: 'low' | 'medium' | 'high';
    message_ids?: number[];
    snippet?: string;
    detail?: string;
  }

  /** 质检维度得分（dimensions_json 映射值） */
  interface QualityDimensionScore {
    score: number;
    weight: number;
    reason?: string;
  }

  // ---- 统计 ----
  interface DashboardStats {
    total_sessions: number;
    total_tickets: number;
    total_customers: number;
    total_agents: number;
    today_tickets: number;
    today_sessions: number;
    today_messages: number;
    open_tickets: number;
    assigned_tickets: number;
    resolved_tickets: number;
    closed_tickets: number;
    online_agents: number;
    busy_agents: number;
    active_sessions: number;
    avg_response_time: number;
    avg_resolution_time: number;
    customer_satisfaction: number;
    ai_usage_today: number;
    weknora_usage_today: number;
  }

  interface RemoteAssistTicketStats {
    total: number;
    open: number;
    resolved: number;
    closed: number;
    resolved_rate: number;
    closed_rate: number;
    avg_close_hours: number;
  }

  interface WorkspaceSession {
    id: string;
    platform: string;
    status: string;
    agent_id?: number;
    agent_name?: string;
    customer_id?: number;
    customer_name?: string;
    started_at: string;
  }

  interface WorkspaceOverview {
    total_active_sessions: number;
    waiting_queue: number;
    online_agents: number;
    busy_agents: number;
    channels: Array<{
      platform: string;
      active_sessions: number;
      waiting_sessions: number;
      avg_response_time: number;
    }>;
    recent_sessions: WorkspaceSession[];
    agent_stats?: {
      available_agents: Array<{ id: number; name?: string }>;
    };
  }

  /** 会话（来自 ticket 关联查询） */
  interface Session {
    id: string;
    platform?: string;
    status?: string;
    customer_id?: number;
    customer_name?: string;
    agent_id?: number;
    agent_name?: string;
    ticket_id?: number;
    started_at?: string;
    ended_at?: string;
  }

  interface CustomerActivity {
    customer_id: number;
    recent_sessions: API.Session[];
    recent_tickets: API.Ticket[];
    recent_messages: Array<{
      id: number;
      session_id: string;
      sender: string;
      content: string;
      created_at: string;
    }>;
  }

  // ---- AI ----
  interface AIStatus {
    provider: string;
    model: string;
    available: boolean;
    latency_ms?: number;
  }

  interface AIQueryResponse {
    answer?: string;
    strategy?: string;
    intent?: string;
    similar_tickets?: unknown[];
    knowledge_docs?: unknown[];
  }

  interface AIMetrics {
    total_queries?: number;
    avg_latency_ms?: number;
    fallback_queries?: number;
  }

  // ---- 集成 ----
  interface Integration {
    id: number;
    name: string;
    slug?: string;
    vendor?: string;
    category?: string;
    summary?: string;
    icon_url?: string;
    capabilities?: string[];
    config_schema?: Record<string, any>;
    iframe_url?: string;
    enabled: boolean;
    last_sync_status?: string;
    created_at: string;
    updated_at?: string;
  }

  // ---- 语音 ----
  interface VoiceProtocol {
    id: string;
    type: string;
    status: string;
    created_at: string;
  }

  interface VoiceRecording {
    id: string;
    call_id: string;
    provider?: string;
    status: string;
    started_at?: string;
    stopped_at?: string;
  }

  interface VoiceTranscript {
    id?: string;
    call_id: string;
    content: string;
    language?: string;
    finalized?: boolean;
    appended_at?: string;
  }

  // ---- 安全管理 ----
  interface UserSecurityPreview {
    user_id: number;
    username: string;
    name: string;
    role: string;
    status: string;
    token_version: number;
    next_token_version: number;
    last_login?: string;
    token_valid_after?: string;
  }

  interface UserSecurityDetail {
    user_id: number;
    role: string;
    status: string;
    token_version: number;
    token_valid_after?: string;
    last_login?: string;
  }

  interface UserSecuritySession {
    session_id: string;
    status: string;
    token_version: number;
    device_fingerprint?: string;
    network_label?: string;
    location_label?: string;
    risk_score?: number;
    risk_level?: string;
    risk_reasons?: string[];
    family_public_ip_count?: number;
    family_device_count?: number;
    active_session_count?: number;
    family_hot_refresh_count?: number;
    reference_session_id?: string;
    ip_drift?: boolean;
    device_drift?: boolean;
    rapid_ip_change?: boolean;
    rapid_device_change?: boolean;
    refresh_recency?: string;
    rapid_refresh_activity?: boolean;
    user_agent?: string;
    client_ip?: string;
    last_seen_at?: string;
    last_refreshed_at?: string;
    revoked_at?: string;
    created_at?: string;
    updated_at?: string;
    is_current?: boolean;
  }

  interface RevokedToken {
    jti: string;
    user_id: number;
    session_id?: string;
    token_use?: string;
    reason?: string;
    expires_at?: string;
    revoked_at?: string;
  }

  interface TicketComment {
    id: number;
    content: string;
    internal?: boolean;
    author?: string;
    created_at: string;
  }

  interface SLAViolation {
    id: number;
    ticket_id: string | number;
    sla_config_id?: number;
    violation_type: string;
    deadline?: string;
    violated_at?: string;
    resolved_at?: string;
    resolved?: boolean;
    created_at: string;
  }

  interface SLAStats {
    total_violations: number;
    unresolved_violations: number;
    resolved_violations: number;
    active_configs: number;
  }

  interface AutomationRun {
    id: number;
    trigger_id?: number;
    status: string;
    event?: string;
    dry_run?: boolean;
    created_at: string;
    completed_at?: string;
  }

  interface TransferQueueRecord {
    id?: string | number;
    session_id: string;
    reason?: string;
    target_skills?: string;
    priority?: string;
    notes?: string;
    status?: string;
    queued_at?: string;
    assigned_at?: string;
    assigned_to?: number;
    created_at?: string;
  }

  interface SessionTransferRecord {
    id: string | number;
    session_id: string;
    from_agent_id?: number;
    to_agent_id?: number;
    reason?: string;
    notes?: string;
    session_summary?: string;
    transferred_at?: string;
    created_at?: string;
  }

  interface SessionTransferResult {
    success?: boolean;
    session_id?: string;
    message?: string;
    target_agent_id?: number;
  }

  interface LeaderboardRecord {
    id: string | number;
    rank: number;
    agent: string;
    agent_id?: number | string;
    department?: string;
    score: number;
    resolved_tickets: number;
    avg_rating: number;
    avg_response_time: number;
    badges?: Array<{
      id: string;
      name: string;
      description: string;
    }>;
  }

  /** Webhook 订阅端点（开放平台）；secret 只在创建/轮换响应中出现一次 */
  interface WebhookEndpoint {
    id: number;
    name: string;
    url: string;
    /** 逗号分隔事件名，空 = 订阅全部已支持事件 */
    events?: string;
    description?: string;
    active?: boolean;
    created_at?: string;
    updated_at?: string;
  }

  /** Webhook 投递记录：pending -> success / failed(带 next_retry_at) / dead */
  interface WebhookDelivery {
    id: number;
    endpoint_id: number;
    event_name: string;
    event_id?: string;
    aggregate_id?: string;
    status: 'pending' | 'success' | 'failed' | 'dead' | string;
    attempt?: number;
    http_status?: number;
    duration_ms?: number;
    last_error?: string;
    payload?: string;
    created_at?: string;
    updated_at?: string;
    delivered_at?: string | null;
    next_retry_at?: string | null;
  }

  /** API Key（开放平台）；明文只在签发响应中返回一次 */
  interface APIKey {
    id: number;
    name: string;
    prefix: string;
    workspace_id?: string;
    scopes?: string;
    created_by?: string;
    last_used_at?: string | null;
    expires_at?: string | null;
    revoked_at?: string | null;
    created_at?: string;
    updated_at?: string;
  }
}
