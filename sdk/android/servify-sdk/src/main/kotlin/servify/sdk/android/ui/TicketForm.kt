package servify.sdk.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
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
import servify.sdk.android.model.TicketReceipt

/**
 * 工单创建表单（M3 刀 3b，D8 依赖白名单内自绘）：标题必填 + 描述选填；
 * ai_summary 由门面自动组装（TicketSummary），表单不收摘要。提交经 [onSubmit]
 * （门面 createTicket）——成功返回回执并收起表单，失败展示重试提示。
 */
@Composable
internal fun TicketFormOverlay(
    onSubmit: suspend (title: String, description: String?) -> TicketReceipt?,
    onDismiss: () -> Unit,
    modifier: Modifier = Modifier,
) {
    var title by remember { mutableStateOf("") }
    var description by remember { mutableStateOf("") }
    var submitting by remember { mutableStateOf(false) }
    var errorText by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()

    Box(
        modifier = modifier
            .fillMaxSize()
            .background(Color.Black.copy(alpha = 0.45f))
            .clickable(enabled = !submitting, onClick = onDismiss)
            .imePadding(),
        contentAlignment = Alignment.Center,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 24.dp)
                .clip(RoundedCornerShape(16.dp))
                .background(ChatThemeDefaults.SurfaceWhite)
                // 消费卡片内点击，避免穿透到 scrim 关闭
                .clickable(
                    interactionSource = remember { MutableInteractionSource() },
                    indication = null,
                    onClick = {},
                )
                .padding(20.dp),
        ) {
            ChatText("创建工单", color = ChatThemeDefaults.TextPrimary, fontSize = 17.sp, fontWeight = FontWeight.SemiBold)
            Spacer(Modifier.height(4.dp))
            ChatText("会话摘要将随工单一并发给客服", color = ChatThemeDefaults.TextSecondary, fontSize = 12.sp)
            Spacer(Modifier.height(16.dp))
            TicketFormField(
                hint = "问题标题",
                value = title,
                onValueChange = { title = it },
                enabled = !submitting,
                singleLine = true,
            )
            Spacer(Modifier.height(10.dp))
            TicketFormField(
                hint = "补充描述（选填）",
                value = description,
                onValueChange = { description = it },
                enabled = !submitting,
                singleLine = false,
            )
            errorText?.let {
                Spacer(Modifier.height(8.dp))
                ChatText(it, color = Color(0xFFDC2626), fontSize = 12.sp)
            }
            Spacer(Modifier.height(16.dp))
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.End,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                ChatText(
                    "取消",
                    color = ChatThemeDefaults.TextSecondary,
                    fontSize = 14.sp,
                    modifier = Modifier
                        .clip(RoundedCornerShape(8.dp))
                        .clickable(enabled = !submitting, onClick = onDismiss)
                        .padding(horizontal = 12.dp, vertical = 8.dp),
                )
                Spacer(Modifier.width(8.dp))
                val canSubmit = title.isNotBlank() && !submitting
                Box(
                    modifier = Modifier
                        .clip(RoundedCornerShape(8.dp))
                        .background(if (canSubmit) LocalServifyPrimary.current else ChatThemeDefaults.Divider)
                        .clickable(enabled = canSubmit) {
                            scope.launch {
                                submitting = true
                                errorText = null
                                val receipt = onSubmit(title.trim(), description.trim().takeIf { it.isNotEmpty() })
                                submitting = false
                                if (receipt != null) {
                                    onDismiss()
                                } else {
                                    errorText = "创建失败，请稍后重试"
                                }
                            }
                        }
                        .padding(horizontal = 16.dp, vertical = 8.dp),
                ) {
                    ChatText(if (submitting) "提交中…" else "提交", color = Color.White, fontSize = 14.sp)
                }
            }
        }
    }
}

@Composable
private fun TicketFormField(
    hint: String,
    value: String,
    onValueChange: (String) -> Unit,
    enabled: Boolean,
    singleLine: Boolean,
) {
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(10.dp))
            .background(ChatThemeDefaults.PageBackground)
            .padding(horizontal = 12.dp, vertical = 10.dp)
            .widthIn(max = 320.dp),
    ) {
        BasicTextField(
            value = value,
            onValueChange = onValueChange,
            enabled = enabled,
            singleLine = singleLine,
            maxLines = if (singleLine) 1 else 4,
            textStyle = TextStyle(color = ChatThemeDefaults.TextPrimary, fontSize = 14.sp),
            cursorBrush = SolidColor(LocalServifyPrimary.current),
            modifier = Modifier.fillMaxWidth(),
            decorationBox = { inner ->
                if (value.isEmpty()) {
                    ChatText(hint, color = ChatThemeDefaults.TextSecondary, fontSize = 14.sp)
                }
                inner()
            },
        )
    }
}
