import { createFileRoute } from "@tanstack/react-router";
import { requireAuth } from "../lib/requireAuth";
import Accounts from "../pages/Accounts";

export const Route = createFileRoute("/accounts")({
  beforeLoad: requireAuth,
  component: Accounts,
});
