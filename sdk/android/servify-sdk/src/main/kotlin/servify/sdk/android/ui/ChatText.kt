package servify.sdk.android.ui

import androidx.compose.foundation.text.BasicTextField
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.TextUnit
import androidx.compose.ui.unit.sp

/**
 * 自绘文本组件（依赖白名单 D9：不引 material/material3，文本渲染以只读
 * [BasicTextField] 承载——多行/换行/字体样式齐备，纯白名单内实现）。
 */
@Composable
internal fun ChatText(
    text: String,
    modifier: Modifier = Modifier,
    color: Color = ChatThemeDefaults.TextPrimary,
    fontSize: TextUnit = 15.sp,
    fontWeight: FontWeight? = null,
    maxLines: Int = Int.MAX_VALUE,
) {
    BasicTextField(
        value = text,
        onValueChange = {},
        readOnly = true,
        textStyle = TextStyle(
            color = color,
            fontSize = fontSize,
            fontWeight = fontWeight,
        ),
        maxLines = maxLines,
        modifier = modifier,
    )
}
