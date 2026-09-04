import { redirect } from "next/navigation";
import { getCurrentSession } from "@/lib/auth";

export default async function MailPage() {
  const session = await getCurrentSession();
  if (!session) redirect("/login");
  redirect("/mail/0/inbox");
}
