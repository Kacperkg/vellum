import { createFileRoute } from "@tanstack/react-router";
import { requireAuth } from "@/features/auth";
import Accounts from "@/features/accounts/pages/Accounts";

export const Route = createFileRoute("/accounts")({
  beforeLoad: requireAuth,
  component: Accounts,
});
