"use client";

import { useActionState, type ReactNode } from "react";
import { logIn, mutate } from "./actions";
import type { ActionState, Provider, Role, Site } from "../lib/api";

type Action = (state: ActionState, data: FormData) => Promise<ActionState>;

function Form({ action, label, children, variant }: {
  action: Action; label: string; children: ReactNode; variant?: string;
}) {
  const [state, submit, pending] = useActionState(action, {});
  return (
    <form action={submit} className="form">
      {children}
      <div className="form-actions">
        <button className={variant} disabled={pending} type="submit">{pending ? "处理中…" : label}</button>
        {state.error && <p role="alert" className="error">{state.error}</p>}
        {state.notice && <p role="status">{state.notice}</p>}
      </div>
      {state.token && <code className="credential" aria-label="访问凭据">{state.token}</code>}
    </form>
  );
}

export function LoginForm() {
  return <Form action={logIn} label="登录">
    <label>访问凭据<input name="token" type="password" autoComplete="current-password" required minLength={32} maxLength={512} /></label>
  </Form>;
}

export function MutationForm({ operation, label, children, variant }: {
  operation: string; label: string; children: ReactNode; variant?: string;
}) {
  return <Form action={mutate.bind(null, operation)} label={label} variant={variant}>{children}</Form>;
}

export function SiteForm({ site }: { site?: Site }) {
  return <MutationForm operation="save-site" label={site ? "保存站点" : "添加站点"}>
    {site && <><input type="hidden" name="id" value={site.id} /><input type="hidden" name="version" value={site.version} /></>}
    <div className="fields">
      <label>站点名称<input name="name" required maxLength={120} defaultValue={site?.name} /></label>
      <label>站点 URL<input name="base_url" type="url" required maxLength={2048} placeholder="https://example.com" defaultValue={site?.base_url} /></label>
      <label>CMS<select name="cms" defaultValue={site?.cms ?? "wordpress"}><option value="wordpress">WordPress</option><option value="generic">通用 CMS</option></select></label>
      <label>国家或地区<input name="country_code" required pattern="[A-Za-z]{2}" placeholder="CN" defaultValue={site?.country_code} /></label>
      <label>语言标签<input name="language_tag" required maxLength={64} placeholder="zh-Hans" defaultValue={site?.language_tag} /></label>
      <label>状态<select name="status" defaultValue={site?.status ?? "active"}>
        <option value="active">启用</option><option value="paused">暂停</option><option value="archived">归档</option>
      </select></label>
    </div>
  </MutationForm>;
}

export function RoleField({ role = "viewer" }: { role?: Role }) {
  return <label>角色<select name="role" defaultValue={role}>
    <option value="viewer">查看者</option><option value="editor">编辑者</option><option value="owner">所有者</option>
  </select></label>;
}

export function ProviderForm({ provider }: { provider?: Provider }) {
  return <MutationForm operation="save-provider" label={provider ? "保存 Provider" : "添加 Provider"}>
    {provider && <><input name="id" type="hidden" value={provider.id} /><input name="version" type="hidden" value={provider.version} /></>}
    <div className="fields">
      <label>名称<input name="name" required maxLength={100} defaultValue={provider?.name} /></label>
      <label>OpenAI 模型<input name="model" required maxLength={100} defaultValue={provider?.model} /></label>
      <label>API 密钥<input name="api_key" type="password" autoComplete="off" minLength={16} maxLength={512}
        required={!provider} placeholder={provider ? "留空保留现有密钥" : undefined} /></label>
      <label>超时（秒）<input name="timeout_seconds" type="number" min={1} max={30} defaultValue={provider?.timeout_seconds ?? 20} required /></label>
      <label>每分钟请求上限<input name="requests_per_minute" type="number" min={1} max={60} defaultValue={provider?.requests_per_minute ?? 5} required /></label>
      <label>每日请求上限<input name="requests_per_day" type="number" min={1} max={10000} defaultValue={provider?.requests_per_day ?? 30} required /></label>
      <label>单次输出 Token 上限<input name="max_output_tokens" type="number" min={16} max={4096} defaultValue={provider?.max_output_tokens ?? 1024} required /></label>
      <label className="checkbox"><input name="enabled" type="checkbox" defaultChecked={provider?.enabled ?? true} />启用</label>
    </div>
  </MutationForm>;
}
