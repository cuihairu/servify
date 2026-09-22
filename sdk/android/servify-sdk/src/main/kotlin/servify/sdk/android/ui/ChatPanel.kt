package servify.sdk.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch
import servify.sdk.android.ServifyChat
import servify.sdk.android.connect.ConnectionState
import servify.sdk.android.model.SenderType

/**
 * 会话面板（D8 三形态的抽屉/全屏共体：形态差异只在宿主容器高度，主体组件同一棵）。
 * 消息流收敛经 [ChatPanelState]（纯 reducer，脱离 Compose 单测）；连接/未读等流直接接门面。
 * 视觉全部自绘（ChatText/Box/Canvas，D9 白名单，不引 material）。
 */
@Composable
fun ChatPanel(
    chat: ServifyChat,
    title: String,
    welcomeText: String?,
    onDismiss: () -> Unit,
    modifier: Modifier = Modifier,
) {
    // 面板打开即会话页可见：清未读（§4.3）。
    LaunchedEffect(chat) { chat.onSessionVisible() }

    val panelState = remember(chat) { ChatPanelState(welcomeText) }
    var version by remember(chat) { mutableIntStateOf(0) }
    val scope = rememberCoroutineScope()
    val connectionState by chat.events.connectionState.collectAsState()
    var statusLine by remember { mutableStateOf<String?>(null) }

    LaunchedEffect(chat) {
        chat.events.messages.collect {
            panelState.onMessage(it)
            version++
        }
    }
    // 转人工状态机渲染（D8：排队提示 / 坐席接入提示，显示最近一条）。
    LaunchedEffect(chat) {
        chat.events.waitingInQueue.collect { statusLine = it }
    }
    LaunchedEffect(chat) {
        chat.events.agentAssigned.collect { statusLine = it.message }
    }

    Column(
        modifier = modifier
            .fillMaxSize()
            .background(ChatThemeDefaults.PageBackground),
    ) {
        TitleBar(title, connectionLabel(connectionState), onDismiss)
        statusLine?.let { StatusLine(it) }
        MessageList(
            items = panelState.items(),
            version = version,
            onHandoff = {
                // V1：协议无客户端转人工帧——点击即发送固定文本消息（服务端兜底链路可达）。
                scope.launch {
                    runCatching { chat.sendMessage("请转人工客服") }
                }
            },
            modifier = Modifier.weight(1f),
        )
        InputBar(
            enabled = connectionState == ConnectionState.Connected,
            onSend = { text ->
                // 发送失败（echo 超时）经 error 流暴露给宿主；面板内静默，可重发。
                scope.launch {
                    runCatching { chat.sendMessage(text) }
                }
            },
        )
    }
}

private fun connectionLabel(state: ConnectionState): String = when (state) {
    ConnectionState.Idle -> ""
    ConnectionState.Connecting -> "连接中…"
    is ConnectionState.Reconnecting -> "重新连接中…（第 ${state.attempt} 次）"
    ConnectionState.Connected -> ""
    ConnectionState.Disconnected -> "连接已断开"
}

@Composable
private fun TitleBar(title: String, status: String, onDismiss: () -> Unit) {
    val primary = LocalServifyPrimary.current
    Column(modifier = Modifier.fillMaxWidth().background(primary).statusBarsPadding()) {
        Row(
            modifier = Modifier.fillMaxWidth().height(52.dp).padding(horizontal = 16.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            ChatText(
                title,
                color = Color.White,
                fontSize = 17.sp,
                fontWeight = FontWeight.SemiBold,
                modifier = Modifier.weight(1f, fill = false),
                maxLines = 1,
            )
            if (status.isNotEmpty()) {
                Spacer(Modifier.widthIn(8.dp))
                ChatText(status, color = Color.White.copy(alpha = 0.85f), fontSize = 12.sp, maxLines = 1)
            }
            Spacer(Modifier.weight(1f))
            ChatText(
                "✕",
                color = Color.White,
                fontSize = 18.sp,
                modifier = Modifier
                    .clip(RoundedCornerShape(8.dp))
                    .clickable(onClick = onDismiss)
                    .padding(6.dp),
            )
        }
    }
}

@Composable
private fun StatusLine(text: String) {
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .background(ChatThemeDefaults.HandoffBadge)
            .padding(horizontal = 16.dp, vertical = 8.dp),
    ) {
        ChatText(text, color = ChatThemeDefaults.TextPrimary, fontSize = 13.sp)
    }
}

@Composable
private fun MessageList(
    items: List<ChatListItem>,
    version: Int,
    onHandoff: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val listState = rememberLazyListState()
    // 新消息（或流式增量）到达即滚到底。
    LaunchedEffect(version) {
        if (items.isNotEmpty()) listState.animateScrollToItem(items.size - 1)
    }
    LazyColumn(state = listState, modifier = modifier.fillMaxWidth()) {
        items(items, key = { item -> item.key }) { item ->
            when (item) {
                is ChatListItem.Welcome -> WelcomeItem(item)
                is ChatListItem.Bubble -> MessageBubble(item, onHandoff)
            }
        }
    }
}

private val ChatListItem.key: String
    get() = when (this) {
        is ChatListItem.Welcome -> "__welcome"
        is ChatListItem.Bubble -> id
    }

@Composable
private fun WelcomeItem(item: ChatListItem.Welcome) {
    Box(modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp)) {
        ChatText(
            item.text,
            color = ChatThemeDefaults.TextSecondary,
            fontSize = 13.sp,
            modifier = Modifier
                .clip(RoundedCornerShape(12.dp))
                .background(ChatThemeDefaults.SurfaceWhite)
                .padding(horizontal = 12.dp, vertical = 8.dp),
        )
    }
}

@Composable
private fun MessageBubble(item: ChatListItem.Bubble, onHandoff: () -> Unit) {
    val right = item.sender.isRightAligned()
    Row(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
        horizontalArrangement = if (right) Arrangement.End else Arrangement.Start,
    ) {
        Column(
            horizontalAlignment = if (right) Alignment.End else Alignment.Start,
            modifier = Modifier.widthIn(max = 280.dp),
        ) {
            if (item.isAiResponse && !right) {
                ChatText(
                    "AI 助手",
                    color = ChatThemeDefaults.TextSecondary,
                    fontSize = 11.sp,
                    modifier = Modifier.padding(bottom = 2.dp, start = 4.dp),
                )
            }
            Box(
                modifier = Modifier
                    .clip(RoundedCornerShape(12.dp))
                    .background(
                        if (right) ChatThemeDefaults.BubbleCustomer else ChatThemeDefaults.BubbleOther,
                    )
                    .padding(horizontal = 12.dp, vertical = 8.dp),
            ) {
                ChatText(
                    if (item.isStreaming) item.content + "…" else item.content,
                    color = if (right) Color.White else ChatThemeDefaults.TextPrimary,
                    fontSize = 15.sp,
                )
            }
            if (item.suggestsHandoff) {
                HandoffButton(onHandoff)
            }
            if (item.sources.isNotEmpty()) {
                SourcesList(item.sources)
            }
        }
    }
}

/** 置信门"转人工"强调按钮（D8：next_action=handoff 驱动）。 */
@Composable
private fun HandoffButton(onClick: () -> Unit) {
    val primary = LocalServifyPrimary.current
    Box(
        modifier = Modifier
            .padding(top = 4.dp)
            .clip(RoundedCornerShape(8.dp))
            .background(primary)
            .clickable(onClick = onClick)
            .padding(horizontal = 12.dp, vertical = 6.dp),
    ) {
        ChatText("转人工", color = Color.White, fontSize = 13.sp, fontWeight = FontWeight.Medium)
    }
}

/** 引用来源列表（D8：按得分降序的展开列表，score 已在映射层排好、null 归底）。 */
@Composable
private fun SourcesList(sources: List<SourceItem>) {
    Column(modifier = Modifier.padding(top = 4.dp)) {
        ChatText("参考来源", color = ChatThemeDefaults.TextSecondary, fontSize = 11.sp)
        sources.forEach { source ->
            ChatText(
                buildString {
                    append("· ")
                    append(source.title)
                    append("（")
                    append(source.documentId)
                    source.score?.let { append(String.format("%.2f", it)) }
                    append("）")
                },
                color = ChatThemeDefaults.TextSecondary,
                fontSize = 12.sp,
                maxLines = 1,
                modifier = Modifier.padding(top = 2.dp),
            )
        }
    }
}

@Composable
private fun InputBar(enabled: Boolean, onSend: (String) -> Unit) {
    var text by remember { mutableStateOf("") }
    val primary = LocalServifyPrimary.current
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .background(ChatThemeDefaults.SurfaceWhite)
            .navigationBarsPadding()
            .imePadding()
            .padding(horizontal = 12.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            modifier = Modifier
                .weight(1f)
                .clip(RoundedCornerShape(20.dp))
                .background(ChatThemeDefaults.PageBackground)
                .padding(horizontal = 12.dp, vertical = 8.dp),
        ) {
            BasicTextField(
                value = text,
                onValueChange = { text = it },
                textStyle = TextStyle(
                    color = ChatThemeDefaults.TextPrimary,
                    fontSize = 15.sp,
                ),
                cursorBrush = SolidColor(primary),
                maxLines = 4,
                modifier = Modifier.fillMaxWidth(),
                decorationBox = { inner ->
                    if (text.isEmpty()) {
                        ChatText("输入消息…", color = ChatThemeDefaults.TextSecondary, fontSize = 15.sp)
                    }
                    inner()
                },
            )
        }
        Spacer(Modifier.widthIn(8.dp))
        val canSend = enabled && text.isNotBlank()
        Box(
            modifier = Modifier
                .clip(RoundedCornerShape(20.dp))
                .background(if (canSend) primary else ChatThemeDefaults.Divider)
                .clickable(enabled = canSend) {
                    onSend(text.trim())
                    text = ""
                }
                .padding(horizontal = 14.dp, vertical = 8.dp),
        ) {
            ChatText("发送", color = Color.White, fontSize = 14.sp)
        }
    }
}
