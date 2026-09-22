package servify.sdk.demo.baseline

import android.os.Bundle

/**
 * 体积基线宿主：与 demo 同一 UI、零 SDK（M1 验收③ 的减数）。
 * 纯 android.app.Activity，不引任何 androidx 依赖，保证差值纯度。
 */
class MainActivity : android.app.Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)
    }
}
