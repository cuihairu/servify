# 知识库 real 验收资源清单（三行，逐家待提供）

> 落档执行（2026-10-30）:按 docs/review/2026-10-30-decisions.md 第 2 项「立即执行动作」第一条,由交付负责人列出三行资源清单,每家记录 provider 地址及版本、测试知识库 ID、凭据注入位置、Servify 测试地址、测试账号、负责人和可用日期;缺项保持「待提供」。本清单为执行载体,不代替代为提供任何外部资源。

状态:**全部待提供**——本次仅列明每项所需的输入与对应验收入口,未获得任何真实端点或凭据;拿到一家完整输入后再按对应脚本跑 real 验收(需真实外部服务,非私网守卫可满足的口径)。

| 项 | RAGFlow | WeKnora | Dify |
| --- | --- | --- | --- |
| 验收脚本 | `scripts/test-ragflow-acceptance.sh` | `scripts/test-weknora-integration.sh` | `scripts/test-dify-integration.sh` |
| real 模式变量 | `RAGFLOW_ACCEPTANCE_MODE=real` | `WEKNORA_ACCEPTANCE_MODE=real` | `DIFY_ACCEPTANCE_MODE=real` |
| provider 地址及版本 | 待提供 | 待提供 | 待提供 |
| 测试知识库 ID | 待提供 | 待提供 | 待提供 |
| 凭据注入位置 | `RAGFLOW_API_KEY` 等(见脚本头) | 待提供(服务端 provider 配置) | 待提供(服务端 provider 配置) |
| Servify 测试地址 | 待提供 | 待提供 | 待提供 |
| 测试账号 | 待提供 | 待提供 | 待提供 |
| 负责人 | 待提供 | 待提供 | 待提供 |
| 可用日期 | 待提供 | 待提供 | 待提供 |

**执行前置核查（真实执行前,每家各自确认）:**
- 专用测试知识库/数据集可写可删(真实上传/更新/删除会改变外部数据,必须使用专用测试资源)。
- 脚本所需配置齐全,`*_ACCEPTANCE_MODE=real` 且 provider 已实际配置到 Servify 实例(仅切脚本模式不能配置好服务端)。
- 证据目录显式设到获准位置;默认脚本会写 `scripts/test-results/...`。
- 脚本的私网地址守卫(ragflow:449 / weknora:236 / dify:229)意味着「本机起一个真服务」不满足现有 real 口径;若只有私网真环境,应另提验收定义调整,不能绕过检查后仍声称按原标准通过。

**验收顺序:** 无指定客户需求时先 RAGFlow,再维护 WeKnora 与 Dify;若已有 Dify/WeKnora 客户验收则对应前置(分批只是顺序,不取消另外两家责任)。每家各自标注通过或仍阻塞;三家所需证据都齐才能关闭父条目(todo.md:19)。

**关闭条件:** 三家各一份脱敏通过记录(真实端点/版本/测试知识库 ID/结果/时间/负责人),证据目录获准落位。
