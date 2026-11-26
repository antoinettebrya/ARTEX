import { randomUUID } from "node:crypto";
import Link from "next/link";
import { api, APIError, isID, sessionToken, type AuditEvent, type Job, type Member, type Principal, type Provider, type Site } from "../lib/api";
import { logOut } from "./actions";
import { LoginForm, MutationForm, ProviderForm, RoleField, SiteForm } from "./forms";

const views = { sites: "站点", jobs: "任务", providers: "Provider", members: "成员", audit: "审计日志" } as const;
const roles = { owner: "所有者", editor: "编辑者", viewer: "查看者" } as const;
const siteStates = { active: "启用", paused: "暂停", archived: "归档" } as const;
const jobStates = { queued: "排队中", running: "运行中", succeeded: "完成", failed: "失败", cancelled: "已取消" } as const;
const jobNames = { site_inventory: "站点资料快照", site_brief: "SEO 启动简报" } as const;
const pageSize = 50;

type View = keyof typeof views;
type Page = { token: string; offset: number; canEdit: boolean; isOwner: boolean };

function url(view: View, offset = 0) {
  return "/?view=" + view + (offset > 0 ? "&offset=" + offset : "");
}

function formatTime(value: string) {
  return new Intl.DateTimeFormat("zh-CN", {
    dateStyle: "short", timeStyle: "short", timeZone: "UTC",
  }).format(new Date(value));
}

function pageQuery(offset: number) {
  return "?limit=" + pageSize + "&offset=" + offset;
}

function Pagination({ view, offset, size }: { view: View; offset: number; size: number }) {
  return <nav className="pagination" aria-label="分页">
    {offset > 0 && <Link href={url(view, Math.max(0, offset - pageSize))}>上一页</Link>}
    {(offset > 0 || size === pageSize) && <span>第 {Math.floor(offset / pageSize) + 1} 页</span>}
    {size === pageSize && offset + pageSize <= 100000 && <Link href={url(view, offset + pageSize)}>下一页</Link>}
  </nav>;
}

function Login({ message }: { message?: string }) {
  return <main className="login">
    <section className="login-card" aria-labelledby="login-title">
      <p className="brand">ARTEX <span>SEO OS</span></p>
      <h1 id="login-title">登录运营后台</h1>
      <p className="muted">输入组织所有者提供的访问凭据。</p>
      {message && <p className="error" role="alert">{message}</p>}
      <LoginForm />
    </section>
  </main>;
}

async function Sites({ token, offset, canEdit }: Page) {
  const { sites } = await api<{ sites: Site[] }>("/sites" + pageQuery(offset), token);
  return <>
    {canEdit && <section className="card"><h2>登记站点</h2><SiteForm /></section>}
    <section className="card">
      <h2>已登记站点</h2>
      {sites.length === 0 ? <p className="muted">本页暂无站点。</p> : <div className="table-scroll">
        <table><thead><tr><th scope="col">站点</th><th scope="col">市场</th><th scope="col">CMS</th><th scope="col">状态</th>{canEdit && <th scope="col">操作</th>}</tr></thead>
          <tbody>{sites.map(site => <tr key={site.id}>
            <td><strong>{site.name}</strong><span className="sub">{site.base_url}</span></td>
            <td>{site.country_code} · {site.language_tag}</td>
            <td>{site.cms === "wordpress" ? "WordPress" : "通用 CMS"}</td>
            <td><span className={"state " + site.status}>{siteStates[site.status]}</span></td>
            {canEdit && <td>
              {site.status === "active" && <Link href={url("jobs") + "&site=" + site.id}>创建任务</Link>}
              <details><summary>编辑 {site.name}</summary><SiteForm site={site} /></details>
            </td>}
          </tr>)}</tbody>
        </table>
      </div>}
      <Pagination view="sites" offset={offset} size={sites.length} />
    </section>
  </>;
}

async function Jobs({ token, offset, canEdit, selectedSite }: Page & { selectedSite: string }) {
  const [listing, chosen, providers] = await Promise.all([
    api<{ jobs: Job[] }>("/jobs" + pageQuery(offset), token),
    canEdit && isID(selectedSite) ? api<{ site: Site }>("/sites/" + selectedSite, token) : null,
    canEdit && isID(selectedSite) ? api<{ providers: Provider[] }>("/providers?limit=200", token) : null,
  ]);
  return <>
    {chosen && chosen.site.status === "active" && <section className="card">
      <h2>为 {chosen.site.name} 创建任务</h2>
      <MutationForm operation="enqueue-job" label="提交任务">
        <input name="site_id" type="hidden" value={chosen.site.id} />
        <input name="idempotency_key" type="hidden" value={randomUUID()} />
        <div className="fields">
          <label>任务类型<select name="kind">
            <option value="site_inventory">{jobNames.site_inventory}</option>
            {(providers?.providers ?? []).some(p => p.enabled) && <option value="site_brief">{jobNames.site_brief}</option>}
          </select></label>
          <label>Provider（SEO 启动简报需要）<select name="provider_id">
            <option value="">选择 Provider</option>
            {providers?.providers.filter(p => p.enabled).map(p => <option value={p.id} key={p.id}>{p.name}</option>)}
          </select></label>
        </div>
      </MutationForm>
    </section>}
    <section className="card">
      <div className="card-heading"><h2>任务队列</h2><a href={url("jobs", offset)}>刷新</a></div>
      {canEdit && !chosen && <p className="muted">从<Link href={url("sites")}>站点列表</Link>选择任务站点。</p>}
      {listing.jobs.length === 0 ? <p className="muted">本页暂无任务。</p> : <div className="table-scroll">
        <table><thead><tr><th scope="col">任务</th><th scope="col">状态</th><th scope="col">尝试次数</th><th scope="col">调度时间（UTC）</th><th scope="col">结果</th></tr></thead>
          <tbody>{listing.jobs.map(job => <tr key={job.id}>
            <td><strong>{jobNames[job.kind]}</strong><span className="sub" title={job.id}>{job.id.slice(0, 12)}</span></td>
            <td><span className={"state " + job.state}>{jobStates[job.state]}</span>{job.error_code && <span className="sub">{job.error_code}</span>}</td>
            <td>{job.attempts}</td><td>{formatTime(job.available_at)}</td>
            <td>{job.result !== undefined && <details><summary>查看结果</summary><pre>{JSON.stringify(job.result, null, 2)}</pre></details>}
              {canEdit && (job.state === "queued" || job.state === "running") && <MutationForm operation="cancel-job" label="取消任务" variant="secondary">
                <input name="id" type="hidden" value={job.id} />
              </MutationForm>}
            </td>
          </tr>)}</tbody>
        </table>
      </div>}
      <Pagination view="jobs" offset={offset} size={listing.jobs.length} />
    </section>
  </>;
}

async function Providers({ token, offset, isOwner }: Page) {
  const { providers } = await api<{ providers: Provider[] }>("/providers" + pageQuery(offset), token);
  return <>
    {isOwner && <section className="card"><h2>接入 OpenAI</h2><ProviderForm /></section>}
    <section className="card">
      <h2>已配置的 Provider</h2>
      {providers.length === 0 && <p className="muted">本页暂无 Provider。</p>}
      {providers.map(provider => <div key={provider.id} className="record">
        <strong>{provider.name}</strong><span className="sub">{provider.model} · {provider.enabled ? "启用" : "停用"}</span>
        <p className="sub">{provider.requests_per_minute} 次/分钟 · {provider.requests_per_day} 次/UTC 日 · {provider.max_output_tokens} 输出 Token/次</p>
        {isOwner && <details><summary>编辑 {provider.name}</summary><ProviderForm provider={provider} /></details>}
      </div>)}
      <Pagination view="providers" offset={offset} size={providers.length} />
    </section>
  </>;
}

async function Members({ token, offset }: Page) {
  const { members } = await api<{ members: Member[] }>("/members" + pageQuery(offset), token);
  return <>
    <section className="card"><h2>添加组织成员</h2>
      <MutationForm operation="add-member" label="添加成员">
        <div className="fields"><label>邮箱<input name="email" type="email" maxLength={254} required /></label><RoleField /></div>
      </MutationForm>
    </section>
    <section className="card">
      <h2>组织成员</h2>
      {members.length === 0 && <p className="muted">本页暂无成员。</p>}
      {members.map(member => <div key={member.user_id} className="record">
        <strong>{member.email}</strong><span className="sub">{roles[member.role]} · {member.enabled ? "凭据有效" : "凭据已撤销"}</span>
        <details><summary>管理 {member.email}</summary>
          <MutationForm operation="set-role" label="更新角色" variant="secondary">
            <input name="id" type="hidden" value={member.user_id} /><RoleField role={member.role} />
          </MutationForm>
          <div className="inline-actions">
            <MutationForm operation="rotate-token" label="签发新凭据" variant="secondary"><input name="id" type="hidden" value={member.user_id} /></MutationForm>
            {member.enabled && <MutationForm operation="revoke-token" label="撤销凭据" variant="secondary"><input name="id" type="hidden" value={member.user_id} /></MutationForm>}
          </div>
        </details>
      </div>)}
      <Pagination view="members" offset={offset} size={members.length} />
    </section>
  </>;
}

async function Audit({ token, offset }: Page) {
  const { events } = await api<{ events: (AuditEvent & { details: unknown })[] }>("/audit" + pageQuery(offset), token);
  return <section className="card"><h2>组织活动</h2>
    {events.length === 0 ? <p className="muted">本页暂无审计记录。</p> : <div className="table-scroll">
      <table><thead><tr><th scope="col">操作</th><th scope="col">身份</th><th scope="col">时间（UTC）</th><th scope="col">详情</th></tr></thead>
        <tbody>{events.map(event => <tr key={event.id}>
          <td><strong>{event.action}</strong><span className="sub">{event.resource_id}</span></td>
          <td>{event.actor_kind}<span className="sub">{event.actor_user_id}</span></td>
          <td>{formatTime(event.created_at)}</td><td><details><summary>查看记录</summary><pre>{JSON.stringify(event.details, null, 2)}</pre></details></td>
        </tr>)}</tbody>
      </table>
    </div>}
    <Pagination view="audit" offset={offset} size={events.length} />
  </section>;
}

async function renderView(view: View, page: Page, selectedSite: string) {
  switch (view) {
    case "sites": return Sites(page);
    case "jobs": return Jobs({ ...page, selectedSite });
    case "providers": return Providers(page);
    case "members": return Members(page);
    case "audit": return Audit(page);
  }
}

export default async function Dashboard({ searchParams }: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const token = await sessionToken();
  if (!token) return <Login />;

  let principal: Principal;
  try {
    principal = await api<Principal>("/me", token);
  } catch (error) {
    return <Login message={error instanceof APIError ? error.message : "暂时无法读取组织信息。"} />;
  }

  const params = await searchParams;
  const isOwner = principal.role === "owner";
  const allowed = (Object.keys(views) as View[]).filter(view => isOwner || (view !== "members" && view !== "audit"));
  const view = allowed.find(view => view === params.view) ?? "sites";
  const offset = typeof params.offset === "string" && /^\d+$/.test(params.offset) ? Math.min(100000, Number(params.offset)) : 0;
  const page = { token, offset, canEdit: principal.role !== "viewer", isOwner };
  let content;
  try {
    content = await renderView(view, page, typeof params.site === "string" ? params.site : "");
  } catch (error) {
    content = <section className="card"><p className="error" role="alert">{error instanceof APIError ? error.message : "暂时无法读取数据。"}</p></section>;
  }

  return <div className="shell">
    <aside className="sidebar">
      <Link className="brand" href="/">ARTEX <span>SEO OS</span></Link>
      <nav aria-label="运营后台">{allowed.map(item =>
        <Link key={item} href={url(item)} aria-current={item === view ? "page" : undefined}>{views[item]}</Link>
      )}</nav>
    </aside>
    <main id="main">
      <header className="topbar">
        <div><h1>{views[view]}</h1><p className="muted" title={principal.organization_id}>组织 {principal.organization_id.slice(0, 8)} · {principal.email ?? "配置凭据"} · {roles[principal.role]}</p></div>
        <form action={logOut}><button type="submit" className="secondary">退出登录</button></form>
      </header>
      {content}
    </main>
  </div>;
}
