# Search Console 集成审计

审计基线 `f2872c0`，工作基线 `c9d42dc` 已包含 Phase 4A 至 `0008`。

| 分类 | 结果 |
| --- | --- |
| A | Webmasters v3、URL Inspection、仅只读授权、PKCE S256 及按日分页符合已公布契约。保留 0008 的每日总量与明细分离、持久化游标、截断与失败状态。 |
| B | 站点级授权无法跨 Property 复用。同意页面总是强制 consent，缺少增量授权与账户级检查，也缺少独立的 URL 检查预算。 |
| C | 旧的站点 OAuth 回调和已有作业须继续识别。 |
| D | 独立真实 OAuth、站点目录、Analytics 与 URL Inspection 尚待验收。 |
| E | 报告原先固定 Web 及 final。URL Inspection 只代表被检查 URL 的观测。查询明细可能省略部分查询，不作为总量。 |
| F | 回调需要继续用恒定路径、单次 state，新增账户与应用版本校验。不能仅凭 OAuth Client ID 或同一 Property 推断两个 Google 授权属于同一用户。 |

[Google Web Server OAuth](https://developers.google.com/identity/protocols/oauth2/web-server) · [Search Analytics](https://developers.google.com/webmaster-tools/v1/searchanalytics/query) · [URL Inspection 限额](https://developers.google.com/webmaster-tools/limits)
