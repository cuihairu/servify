package servify.sdk.android.model

/** transfer_notification 的事件载荷（平台规格 §4.3 agentAssigned）。 */
data class AgentAssignment(val agentId: Long, val message: String)
