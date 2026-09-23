package servify.sdk.android.model

/**
 * 工单创建回执（M3 会话页工单入口）：服务端 201 响应体 id 的透传，供面板提示行
 * （"工单 #N 已创建"）与宿主后续流转使用。
 */
data class TicketReceipt(val ticketId: Long)
