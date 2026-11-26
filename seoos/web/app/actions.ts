"use server";

import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { revalidatePath } from "next/cache";
import { api, APIError, cookieName, isID, sessionToken, type ActionState } from "../lib/api";

function field(data: FormData, name: string): string {
  const value = data.get(name);
  return typeof value === "string" ? value : "";
}

function id(data: FormData, name: string): string {
  const value = field(data, name);
  if (!isID(value)) throw new APIError(400, "invalid_request");
  return value;
}

function readableError(error: unknown): ActionState {
  return { error: error instanceof APIError ? error.message : "操作未完成，请重试。" };
}

export async function logIn(_: ActionState, data: FormData): Promise<ActionState> {
  const token = field(data, "token").trim();
  if (!/^[\x21-\x7e]{32,512}$/.test(token)) return { error: "请输入有效的访问凭据。" };
  try {
    await api("/me", token);
  } catch (error) {
    return error instanceof APIError && error.status === 401
      ? { error: "访问凭据无效或已撤销。" }
      : readableError(error);
  }
  (await cookies()).set(cookieName, token, {
    httpOnly: true,
    sameSite: "lax",
    secure: process.env.NODE_ENV === "production",
    maxAge: 8 * 60 * 60,
    path: "/",
  });
  redirect("/");
}

export async function logOut() {
  (await cookies()).delete(cookieName);
  redirect("/");
}

export async function mutate(operation: string, _: ActionState, data: FormData): Promise<ActionState> {
  const token = await sessionToken();
  if (!token) return { error: "请重新登录。" };
  let state: ActionState;
  try {
    switch (operation) {
      case "save-site": {
        const site = {
          name: field(data, "name"),
          base_url: field(data, "base_url"),
          cms: field(data, "cms"),
          country_code: field(data, "country_code"),
          language_tag: field(data, "language_tag"),
          status: field(data, "status"),
        };
        if (field(data, "id")) {
          await api("/sites/" + id(data, "id"), token, "PUT", {
            ...site, version: Number(field(data, "version")),
          });
        } else {
          await api("/sites", token, "POST", site);
        }
        state = { notice: "站点已保存。" };
        break;
      }
      case "enqueue-job": {
        await api("/jobs", token, "POST", {
          site_id: id(data, "site_id"),
          kind: field(data, "kind"),
          provider_id: field(data, "provider_id") || undefined,
          idempotency_key: field(data, "idempotency_key"),
        });
        state = { notice: "任务已提交。" };
        break;
      }
      case "cancel-job": {
        await api("/jobs/" + id(data, "id"), token, "DELETE");
        state = { notice: "任务已取消。" };
        break;
      }
      case "save-provider": {
        const existing = field(data, "id") !== "";
        await api(existing ? "/providers/" + id(data, "id") : "/providers", token, existing ? "PUT" : "POST", {
          name: field(data, "name"),
          model: field(data, "model"),
          api_key: field(data, "api_key") || undefined,
          timeout_seconds: Number(field(data, "timeout_seconds")),
          requests_per_minute: Number(field(data, "requests_per_minute")),
          requests_per_day: Number(field(data, "requests_per_day")),
          max_output_tokens: Number(field(data, "max_output_tokens")),
          enabled: data.has("enabled"),
          version: existing ? Number(field(data, "version")) : undefined,
        });
        state = { notice: "Provider 已保存。" };
        break;
      }
      case "add-member": {
        const credential = await api<{ token: string }>("/members", token, "POST", {
          email: field(data, "email"),
          role: field(data, "role"),
        });
        state = { notice: "成员已添加。请保存一次性显示的访问凭据。", token: credential.token };
        break;
      }
      case "set-role": {
        await api("/members/" + id(data, "id") + "/role", token, "PUT", { role: field(data, "role") });
        state = { notice: "角色已更新。" };
        break;
      }
      case "rotate-token": {
        const credential = await api<{ token: string }>("/members/" + id(data, "id") + "/token", token, "POST");
        state = { notice: "新凭据已生效。请保存一次性显示的访问凭据。", token: credential.token };
        break;
      }
      case "revoke-token": {
        await api("/members/" + id(data, "id") + "/token", token, "DELETE");
        state = { notice: "凭据已撤销。" };
        break;
      }
      default:
        return { error: "操作无效。" };
    }
  } catch (error) {
    return readableError(error);
  }
  revalidatePath("/");
  return state;
}
