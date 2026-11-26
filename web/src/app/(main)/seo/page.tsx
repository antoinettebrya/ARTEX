import { redirect } from "next/navigation";

import { SEO_HOME } from "@/lib/seo/routes";

export default function Page() {
  redirect(SEO_HOME);
}
