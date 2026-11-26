import type { Metadata } from "next";

import { AuthScreen } from "../../_components/auth-screen";
import { SeoLogin } from "./seo-login";

export const metadata: Metadata = {
  title: "登录 · ARTEX SEO OS",
};

export default function LoginPage() {
  return (
    <AuthScreen title="登录运营后台" description="请输入组织所有者提供的访问凭据。">
      <SeoLogin />
    </AuthScreen>
  );
}
