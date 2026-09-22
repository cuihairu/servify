package servify.sdk.demo

import android.os.Bundle
import androidx.activity.ComponentActivity
import servify.sdk.android.ServifyChat
import servify.sdk.android.ServifyConfig

/**
 * M1 验收②：宿主集成 ≤10 行。核心集成面是下面 onCreate 里的三行
 * （create → show）：浮钮/面板/连接全部由 SDK 自己接管。
 */
class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        val chat = ServifyChat.create(
            this,
            ServifyConfig(apiUrl = "http://10.0.2.2:18099"),
        )
        chat.show(this)
    }
}
