"use client";

import { useState } from "react";

import { Ellipsis, KeyRound, Pencil, Plus, UserX } from "lucide-react";
import { toast } from "sonner";

import { CopyButton } from "@/components/copy-button";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { TableCell, TableRow } from "@/components/ui/table";
import { errorMessage, type Member, type Role, roles, seoRequest } from "@/lib/seo/client";

import { useSeoSession } from "./session";
import { ConfirmChange, Field, FormDialog, field, PageFrame, RecordsTable, StateBadge, useSeoList } from "./shared";

function RoleField({ role = "viewer" }: { role?: Role }) {
  return (
    <Field id="member-role" label="角色">
      <NativeSelect id="member-role" name="role" className="w-full" defaultValue={role}>
        {Object.entries(roles).map(([value, label]) => (
          <NativeSelectOption key={value} value={value}>
            {label}
          </NativeSelectOption>
        ))}
      </NativeSelect>
    </Field>
  );
}

type PendingChange = { member: Member; operation: "rotate" | "revoke" };

export function Members() {
  const { principal, refresh } = useSeoSession();
  const list = useSeoList<Member>("members");
  const [add, setAdd] = useState(false);
  const [edit, setEdit] = useState<Member>();
  const [pending, setPending] = useState<PendingChange>();
  const [secret, setSecret] = useState("");

  async function addMember(data: FormData) {
    const created = await seoRequest<{ token: string }>("/members", {
      method: "POST",
      body: { email: field(data, "email"), role: field(data, "role") },
    });
    setSecret(created.token);
    list.refresh();
    toast.success("成员已添加");
  }

  async function changeCredential(change: PendingChange) {
    const path = `/members/${change.member.user_id}/token`;
    const self = change.member.user_id === principal.user_id;
    if (change.operation === "revoke") {
      await seoRequest<void>(path, { method: "DELETE" });
      toast.success("凭据已撤销");
      if (self) refresh();
    } else {
      const { token } = await seoRequest<{ token: string }>(path, { method: "POST" });
      setSecret(token);
      toast.success("新凭据已签发");
      if (self) {
        try {
          await seoRequest<void>("/session", { method: "POST", body: { token } });
        } catch (error) {
          // The credential exists even if replacing the browser session fails.
          toast.error(errorMessage(error));
        }
      }
    }
    list.refresh();
  }

  return (
    <PageFrame
      title="成员与权限"
      description="管理组织角色和访问凭据。"
      actions={
        <Button variant="outline" size="sm" onClick={() => setAdd(true)}>
          <Plus /> 添加成员
        </Button>
      }
    >
      <RecordsTable label="组织成员" columns={["邮箱", "角色", "凭据", "操作"]} empty="暂无组织成员" list={list}>
        {list.records.map((member) => (
          <TableRow key={member.user_id}>
            <TableCell className="px-3 font-medium">{member.email}</TableCell>
            <TableCell className="px-3">{roles[member.role]}</TableCell>
            <TableCell className="px-3">
              <StateBadge label={member.enabled ? "有效" : "已撤销"} tone={member.enabled ? "secondary" : "outline"} />
            </TableCell>
            <TableCell className="px-3">
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button size="icon-sm" variant="ghost" aria-label={`管理 ${member.email}`}>
                    <Ellipsis />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem onSelect={() => setEdit(member)}>
                    <Pencil /> 更新角色
                  </DropdownMenuItem>
                  <DropdownMenuItem onSelect={() => setPending({ member, operation: "rotate" })}>
                    <KeyRound /> 签发新凭据
                  </DropdownMenuItem>
                  {member.enabled && (
                    <DropdownMenuItem
                      variant="destructive"
                      onSelect={() => setPending({ member, operation: "revoke" })}
                    >
                      <UserX /> 撤销凭据
                    </DropdownMenuItem>
                  )}
                </DropdownMenuContent>
              </DropdownMenu>
            </TableCell>
          </TableRow>
        ))}
      </RecordsTable>
      {add && (
        <FormDialog
          title="添加成员"
          description="提交后显示一次性访问凭据。"
          submitLabel="添加成员"
          onClose={() => setAdd(false)}
          onSubmit={addMember}
        >
          <Field id="member-email" label="邮箱">
            <Input id="member-email" name="email" type="email" maxLength={254} required />
          </Field>
          <RoleField />
        </FormDialog>
      )}
      {edit && (
        <FormDialog
          title="更新角色"
          description={edit.email}
          submitLabel="保存角色"
          onClose={() => setEdit(undefined)}
          onSubmit={async (data) => {
            await seoRequest(`/members/${edit.user_id}/role`, {
              method: "PUT",
              body: { role: field(data, "role") },
            });
            list.refresh();
            if (edit.user_id === principal.user_id) refresh();
            toast.success("角色已更新");
          }}
        >
          <RoleField role={edit.role} />
        </FormDialog>
      )}
      {pending && (
        <ConfirmChange
          title={pending.operation === "rotate" ? "签发新凭据" : "撤销凭据"}
          description={`${pending.member.email} 的现有凭据将立即失效。`}
          onClose={() => setPending(undefined)}
          onConfirm={() => changeCredential(pending)}
        />
      )}
      {secret && (
        <Dialog
          open
          onOpenChange={(open) => {
            if (!open) setSecret("");
          }}
        >
          <DialogContent className="sm:max-w-lg">
            <DialogHeader>
              <DialogTitle>访问凭据</DialogTitle>
              <DialogDescription>请保存一次性显示的凭据。</DialogDescription>
            </DialogHeader>
            <code
              data-testid="issued-credential"
              className="break-all rounded-md border bg-muted/50 p-3 font-mono text-xs"
            >
              {secret}
            </code>
            <DialogFooter>
              <CopyButton text={secret} label="复制凭据" />
              <DialogClose asChild>
                <Button type="button">完成</Button>
              </DialogClose>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </PageFrame>
  );
}
